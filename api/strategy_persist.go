package api

import (
	"encoding/json"
	"log"
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
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updatedAt"`
}

// SaveStrategyState 保存策略状态到 DB（启动策略后调用）
func SaveStrategyState(strategyType, symbol string, config interface{}) {
	if DB == nil {
		return
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		log.Printf("[StrategyPersist] Failed to marshal config: %v", err)
		return
	}

	state := StrategyState{}
	result := DB.Where("strategy_type = ? AND symbol = ?", strategyType, symbol).First(&state)

	if result.Error != nil {
		// 新建记录
		state = StrategyState{
			StrategyType: strategyType,
			Symbol:       symbol,
			ConfigJSON:   string(configBytes),
			Status:       "ACTIVE",
			StartedAt:    time.Now(),
		}
		DB.Create(&state)
	} else {
		// 更新已有记录
		DB.Model(&state).Updates(map[string]interface{}{
			"config_json": string(configBytes),
			"status":      "ACTIVE",
			"started_at":  time.Now(),
			"stopped_at":  nil,
		})
	}

	log.Printf("[StrategyPersist] Saved %s/%s as ACTIVE", strategyType, symbol)
}

// MarkStrategyStopped 标记策略已停止
func MarkStrategyStopped(strategyType, symbol string) {
	if DB == nil {
		return
	}

	now := time.Now()
	DB.Model(&StrategyState{}).
		Where("strategy_type = ? AND symbol = ? AND status = ?", strategyType, symbol, "ACTIVE").
		Updates(map[string]interface{}{
			"status":     "STOPPED",
			"stopped_at": now,
		})

	log.Printf("[StrategyPersist] Marked %s/%s as STOPPED", strategyType, symbol)
}

// RecoverStrategies 恢复所有 ACTIVE 策略（程序启动时调用）
func RecoverStrategies() {
	if DB == nil {
		return
	}

	var states []StrategyState
	if err := DB.Where("status = ?", "ACTIVE").Find(&states).Error; err != nil {
		log.Printf("[StrategyPersist] Failed to load active strategies: %v", err)
		return
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
	if DB == nil {
		return nil
	}
	var states []StrategyState
	DB.Order("updated_at DESC").Find(&states)
	return states
}
