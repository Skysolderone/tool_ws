package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const strategyStateRedisKeySuffix = "strategy:state:v1"

var strategyStateRedis struct {
	mu     sync.RWMutex
	client *redis.Client
	key    string
}

func InitStrategyStateRedis(cfg RedisConfig) {
	if !cfg.Enabled {
		disableStrategyStateRedis()
		return
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		log.Printf("[StrategyPersist] Redis cache disabled: empty addr")
		disableStrategyStateRedis()
		return
	}

	keyPrefix := strings.TrimSpace(cfg.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = "tool:"
	}
	key := keyPrefix + strategyStateRedisKeySuffix

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[StrategyPersist] Redis cache disabled: ping failed addr=%s err=%v", addr, err)
		_ = client.Close()
		disableStrategyStateRedis()
		return
	}

	strategyStateRedis.mu.Lock()
	old := strategyStateRedis.client
	strategyStateRedis.client = client
	strategyStateRedis.key = key
	strategyStateRedis.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	log.Printf("[StrategyPersist] Redis cache enabled addr=%s db=%d key=%s", addr, cfg.DB, key)
}

func disableStrategyStateRedis() {
	strategyStateRedis.mu.Lock()
	old := strategyStateRedis.client
	strategyStateRedis.client = nil
	strategyStateRedis.key = ""
	strategyStateRedis.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func getStrategyStateRedis() (*redis.Client, string) {
	strategyStateRedis.mu.RLock()
	defer strategyStateRedis.mu.RUnlock()
	return strategyStateRedis.client, strategyStateRedis.key
}

func isStrategyStateRedisEnabled() bool {
	client, key := getStrategyStateRedis()
	return client != nil && key != ""
}

func strategyStateField(strategyType, symbol string) string {
	t := strings.ToLower(strings.TrimSpace(strategyType))
	s := strings.ToUpper(strings.TrimSpace(symbol))
	return t + ":" + s
}

func upsertStrategyStateRedis(state StrategyState) {
	client, key := getStrategyStateRedis()
	if client == nil || key == "" {
		return
	}

	field := strategyStateField(state.StrategyType, state.Symbol)
	if field == ":" {
		return
	}

	raw, err := json.Marshal(state)
	if err != nil {
		log.Printf("[StrategyPersist] Redis marshal failed for %s/%s: %v", state.StrategyType, state.Symbol, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.HSet(ctx, key, field, raw).Err(); err != nil {
		log.Printf("[StrategyPersist] Redis upsert failed for %s/%s: %v", state.StrategyType, state.Symbol, err)
	}
}

func getStrategyStateRedisOne(strategyType, symbol string) (*StrategyState, error) {
	client, key := getStrategyStateRedis()
	if client == nil || key == "" {
		return nil, nil
	}
	field := strategyStateField(strategyType, symbol)
	if field == ":" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := client.HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis hget %s/%s: %w", strategyType, symbol, err)
	}
	var state StrategyState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("redis decode %s/%s: %w", strategyType, symbol, err)
	}
	return &state, nil
}

func loadStrategyStatesFromRedis() ([]StrategyState, error) {
	client, key := getStrategyStateRedis()
	if client == nil || key == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	kv, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis hgetall %s: %w", key, err)
	}
	if len(kv) == 0 {
		return []StrategyState{}, nil
	}

	out := make([]StrategyState, 0, len(kv))
	for _, raw := range kv {
		var s StrategyState
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			log.Printf("[StrategyPersist] Redis decode state failed: %v", err)
			continue
		}
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func replaceStrategyStatesInRedis(states []StrategyState) {
	client, key := getStrategyStateRedis()
	if client == nil || key == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pipe := client.TxPipeline()
	pipe.Del(ctx, key)
	for _, s := range states {
		field := strategyStateField(s.StrategyType, s.Symbol)
		if field == ":" {
			continue
		}
		raw, err := json.Marshal(s)
		if err != nil {
			log.Printf("[StrategyPersist] Redis marshal failed for %s/%s during replace: %v", s.StrategyType, s.Symbol, err)
			continue
		}
		pipe.HSet(ctx, key, field, raw)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[StrategyPersist] Redis replace failed: %v", err)
	}
}

func syncStrategyStateFromDB(strategyType, symbol string) {
	if DB == nil {
		return
	}
	var state StrategyState
	if err := DB.Where("strategy_type = ? AND symbol = ?", strategyType, symbol).First(&state).Error; err != nil {
		return
	}
	upsertStrategyStateRedis(state)
}
