package api

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"
)

// StrategyState 策略运行状态持久化
type StrategyState struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	StrategyType string     `gorm:"type:varchar(40);uniqueIndex:idx_strategy_key" json:"strategyType"` // scalp/signal/doji/grid/dca/autoscale/funding
	Symbol       string     `gorm:"type:varchar(20);uniqueIndex:idx_strategy_key" json:"symbol"`       // 交易对，funding 类全局策略用 "*"
	ConfigJSON   string     `gorm:"type:text" json:"configJson"`                                       // JSON 序列化的配置
	Status       string     `gorm:"type:varchar(20);index" json:"status"`                              // ACTIVE / STOPPED
	StartedAt    time.Time  `json:"startedAt"`
	StoppedAt    *time.Time `json:"stoppedAt,omitempty"`
	Demoted      bool       `json:"demoted"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updatedAt"`
}

// SaveStrategyState 保存策略状态到 DB（启动策略后调用）
func SaveStrategyState(strategyType, symbol string, config interface{}) {
	strategyType = strings.ToLower(strings.TrimSpace(strategyType))
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if strategyType == "" || symbol == "" {
		return
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		log.Printf("[StrategyPersist] Failed to marshal config: %v", err)
		return
	}

	now := time.Now()
	state := StrategyState{
		StrategyType: strategyType,
		Symbol:       symbol,
		ConfigJSON:   string(configBytes),
		Status:       "ACTIVE",
		StartedAt:    now,
		StoppedAt:    nil,
		UpdatedAt:    now,
	}

	if DB != nil {
		dbState := StrategyState{}
		result := DB.Where("strategy_type = ? AND symbol = ?", strategyType, symbol).First(&dbState)

		if result.Error != nil {
			if err := DB.Create(&state).Error; err != nil {
				log.Printf("[StrategyPersist] Failed to save %s/%s to DB: %v", strategyType, symbol, err)
			}
		} else {
			if err := DB.Model(&dbState).Updates(map[string]interface{}{
				"config_json": string(configBytes),
				"status":      "ACTIVE",
				"started_at":  now,
				"stopped_at":  nil,
			}).Error; err != nil {
				log.Printf("[StrategyPersist] Failed to update %s/%s in DB: %v", strategyType, symbol, err)
			}
			if err := DB.Where("strategy_type = ? AND symbol = ?", strategyType, symbol).First(&state).Error; err != nil {
				state = dbState
				state.ConfigJSON = string(configBytes)
				state.Status = "ACTIVE"
				state.StartedAt = now
				state.StoppedAt = nil
				state.UpdatedAt = now
			}
		}
	}

	upsertStrategyStateRedis(state)
	log.Printf("[StrategyPersist] Saved %s/%s as ACTIVE", strategyType, symbol)
}

// MarkStrategyStopped 标记策略已停止
func MarkStrategyStopped(strategyType, symbol string) {
	strategyType = strings.ToLower(strings.TrimSpace(strategyType))
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if strategyType == "" || symbol == "" {
		return
	}

	now := time.Now()
	if DB != nil {
		if err := DB.Model(&StrategyState{}).
			Where("strategy_type = ? AND symbol = ? AND status = ?", strategyType, symbol, "ACTIVE").
			Updates(map[string]interface{}{
				"status":     "STOPPED",
				"stopped_at": now,
			}).Error; err != nil {
			log.Printf("[StrategyPersist] Failed to mark STOPPED in DB for %s/%s: %v", strategyType, symbol, err)
		}
	}

	if s, err := getStrategyStateRedisOne(strategyType, symbol); err == nil && s != nil {
		s.Status = "STOPPED"
		s.StoppedAt = &now
		s.UpdatedAt = now
		upsertStrategyStateRedis(*s)
	} else {
		syncStrategyStateFromDB(strategyType, symbol)
		if s2, e2 := getStrategyStateRedisOne(strategyType, symbol); e2 == nil && s2 != nil {
			s2.Status = "STOPPED"
			s2.StoppedAt = &now
			s2.UpdatedAt = now
			upsertStrategyStateRedis(*s2)
		} else {
			upsertStrategyStateRedis(StrategyState{
				StrategyType: strategyType,
				Symbol:       symbol,
				Status:       "STOPPED",
				StoppedAt:    &now,
				UpdatedAt:    now,
			})
		}
	}

	log.Printf("[StrategyPersist] Marked %s/%s as STOPPED", strategyType, symbol)
}

// RecoverStrategies 恢复所有 ACTIVE 策略（程序启动时调用）
func RecoverStrategies() {
	states := make([]StrategyState, 0)
	loadedFromRedis := false
	if redisStates, err := loadStrategyStatesFromRedis(); err != nil {
		log.Printf("[StrategyPersist] Redis load failed, fallback DB: %v", err)
	} else if len(redisStates) > 0 {
		for _, s := range redisStates {
			if strings.EqualFold(s.Status, "ACTIVE") {
				states = append(states, s)
			}
		}
		if len(states) > 0 {
			loadedFromRedis = true
			log.Printf("[StrategyPersist] Loaded %d active strategies from Redis", len(states))
		} else {
			log.Printf("[StrategyPersist] Redis has no ACTIVE strategy, fallback DB")
		}
	}

	if !loadedFromRedis {
		if DB == nil {
			return
		}
		if err := DB.Where("status = ?", "ACTIVE").Find(&states).Error; err != nil {
			log.Printf("[StrategyPersist] Failed to load active strategies from DB: %v", err)
			return
		}
		if len(states) > 0 {
			// DB 兜底回填 Redis。
			var allStates []StrategyState
			if err := DB.Order("updated_at DESC").Find(&allStates).Error; err == nil {
				replaceStrategyStatesInRedis(allStates)
			}
		}
	}

	if len(states) == 0 {
		log.Println("[StrategyPersist] No active strategies to recover")
		return
	}

	log.Printf("[StrategyPersist] Recovering %d active strategies...", len(states))

	for _, state := range states {
		var err error
		switch state.StrategyType {
		case "scalp":
			var cfg ScalpConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartScalp(cfg)
			}
		case "signal":
			var cfg SignalConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartSignalStrategy(cfg)
			}
		case "doji":
			var cfg DojiConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartDojiStrategy(cfg)
			}
		case "grid":
			var cfg GridConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartGrid(cfg)
			}
		case "dca":
			var cfg DCAConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartDCA(cfg)
			}
		case "autoscale":
			var cfg AutoScaleConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartAutoScale(cfg)
			}
		case "funding":
			var cfg FundingRateConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartFundingMonitor(cfg)
			}
		case "news_sentiment":
			var cfg NewsSentimentConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartNewsSentiment(cfg)
			}
		case "liq_cascade":
			var cfg LiqCascadeConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartLiqCascade(cfg)
			}
		case "funding_arb":
			var cfg FundingArbConfig
			if json.Unmarshal([]byte(state.ConfigJSON), &cfg) == nil {
				err = StartFundingArb(cfg)
			}
		default:
			log.Printf("[StrategyPersist] Unknown strategy type: %s", state.StrategyType)
			continue
		}

		if err != nil {
			log.Printf("[StrategyPersist] Failed to recover %s/%s: %v", state.StrategyType, state.Symbol, err)
			// 恢复失败时标记为 STOPPED，避免下次重启再尝试
			MarkStrategyStopped(state.StrategyType, state.Symbol)
		} else {
			log.Printf("[StrategyPersist] Recovered %s/%s", state.StrategyType, state.Symbol)
		}
	}
}

// GetAllStrategyStates 获取所有策略状态（按更新时间倒序）
func GetAllStrategyStates() []StrategyState {
	if redisStates, err := loadStrategyStatesFromRedis(); err == nil {
		if len(redisStates) > 0 {
			return redisStates
		}
		if isStrategyStateRedisEnabled() && DB != nil {
			var dbStates []StrategyState
			if err := DB.Order("updated_at DESC").Find(&dbStates).Error; err == nil && len(dbStates) > 0 {
				replaceStrategyStatesInRedis(dbStates)
				return dbStates
			}
		}
	} else {
		log.Printf("[StrategyPersist] Redis query failed, fallback DB: %v", err)
	}

	if DB == nil {
		return nil
	}
	var states []StrategyState
	if err := DB.Order("updated_at DESC").Find(&states).Error; err != nil {
		log.Printf("[StrategyPersist] DB query states failed: %v", err)
		return nil
	}
	if len(states) > 0 {
		sort.Slice(states, func(i, j int) bool {
			return states[i].UpdatedAt.After(states[j].UpdatedAt)
		})
		replaceStrategyStatesInRedis(states)
	}
	return states
}
