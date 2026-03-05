package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const localTPSLRedisKeySuffix = "tpsl:active:v1"

var localTPSLRedisState struct {
	mu     sync.RWMutex
	client *redis.Client
	key    string
}

// InitLocalTPSLRedis 初始化本地 TPSL 的 Redis 存储（可选）。
func InitLocalTPSLRedis(cfg RedisConfig) {
	if !cfg.Enabled {
		disableLocalTPSLRedis()
		return
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		log.Printf("[LocalTPSL] Redis cache disabled: empty addr")
		disableLocalTPSLRedis()
		return
	}

	keyPrefix := strings.TrimSpace(cfg.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = "tool:"
	}
	key := keyPrefix + localTPSLRedisKeySuffix

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
		log.Printf("[LocalTPSL] Redis cache disabled: ping failed addr=%s err=%v", addr, err)
		_ = client.Close()
		disableLocalTPSLRedis()
		return
	}

	localTPSLRedisState.mu.Lock()
	old := localTPSLRedisState.client
	localTPSLRedisState.client = client
	localTPSLRedisState.key = key
	localTPSLRedisState.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	log.Printf("[LocalTPSL] Redis cache enabled addr=%s db=%d key=%s", addr, cfg.DB, key)
}

func disableLocalTPSLRedis() {
	localTPSLRedisState.mu.Lock()
	old := localTPSLRedisState.client
	localTPSLRedisState.client = nil
	localTPSLRedisState.key = ""
	localTPSLRedisState.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func getLocalTPSLRedisState() (*redis.Client, string) {
	localTPSLRedisState.mu.RLock()
	defer localTPSLRedisState.mu.RUnlock()
	return localTPSLRedisState.client, localTPSLRedisState.key
}

func isLocalTPSLRedisEnabled() bool {
	client, key := getLocalTPSLRedisState()
	return client != nil && key != ""
}

func loadActiveTPSLFromRedis() ([]*LocalTPSLCondition, error) {
	client, key := getLocalTPSLRedisState()
	if client == nil || key == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	kv, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("hgetall %s: %w", key, err)
	}
	if len(kv) == 0 {
		return []*LocalTPSLCondition{}, nil
	}

	result := make([]*LocalTPSLCondition, 0, len(kv))
	for _, raw := range kv {
		var cond LocalTPSLCondition
		if err := json.Unmarshal([]byte(raw), &cond); err != nil {
			log.Printf("[LocalTPSL] Redis decode failed: %v", err)
			continue
		}
		if strings.ToUpper(strings.TrimSpace(cond.Status)) != "ACTIVE" {
			continue
		}
		c := cond
		result = append(result, &c)
	}
	return result, nil
}

func upsertActiveTPSLToRedis(cond *LocalTPSLCondition) {
	if cond == nil {
		return
	}
	client, key := getLocalTPSLRedisState()
	if client == nil || key == "" {
		return
	}

	field := strconv.FormatUint(uint64(cond.ID), 10)
	if strings.ToUpper(strings.TrimSpace(cond.Status)) != "ACTIVE" {
		removeTPSLFromRedis(cond.ID)
		return
	}

	raw, err := json.Marshal(cond)
	if err != nil {
		log.Printf("[LocalTPSL] Redis encode failed for id=%d: %v", cond.ID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.HSet(ctx, key, field, raw).Err(); err != nil {
		log.Printf("[LocalTPSL] Redis upsert failed for id=%d: %v", cond.ID, err)
	}
}

func removeTPSLFromRedis(condID uint) {
	client, key := getLocalTPSLRedisState()
	if client == nil || key == "" || condID == 0 {
		return
	}

	field := strconv.FormatUint(uint64(condID), 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.HDel(ctx, key, field).Err(); err != nil {
		log.Printf("[LocalTPSL] Redis remove failed for id=%d: %v", condID, err)
	}
}

func replaceActiveTPSLInRedis(conds []*LocalTPSLCondition) {
	client, key := getLocalTPSLRedisState()
	if client == nil || key == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pipe := client.TxPipeline()
	pipe.Del(ctx, key)
	for _, cond := range conds {
		if cond == nil || strings.ToUpper(strings.TrimSpace(cond.Status)) != "ACTIVE" {
			continue
		}
		raw, err := json.Marshal(cond)
		if err != nil {
			log.Printf("[LocalTPSL] Redis encode failed during replace id=%d: %v", cond.ID, err)
			continue
		}
		field := strconv.FormatUint(uint64(cond.ID), 10)
		pipe.HSet(ctx, key, field, raw)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[LocalTPSL] Redis replace failed: %v", err)
	}
}
