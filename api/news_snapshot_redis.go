package api

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const newsSnapshotRedisKeySuffix = "news:snapshot:v1"

type newsSnapshotRedisPayload struct {
	UpdatedAt int64                 `json:"updatedAt"`
	Data      map[string][]newsItem `json:"data"`
}

var (
	newsSnapshotRedisState struct {
		mu     sync.RWMutex
		client *redis.Client
		key    string
		ttl    time.Duration
	}
	newsSnapshotRedisLoadOnce sync.Once
)

// InitNewsSnapshotRedis 初始化新闻快照 Redis 存储（可选）。
func InitNewsSnapshotRedis(cfg RedisConfig) {
	if !cfg.Enabled {
		disableNewsSnapshotRedis()
		return
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		log.Printf("[WsNews] Redis snapshot cache disabled: empty addr")
		disableNewsSnapshotRedis()
		return
	}

	keyPrefix := strings.TrimSpace(cfg.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = "tool:"
	}
	key := keyPrefix + newsSnapshotRedisKeySuffix

	ttl := time.Duration(cfg.SnapshotTTLSeconds) * time.Second
	if cfg.SnapshotTTLSeconds < 0 {
		ttl = 0
	}

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
		log.Printf("[WsNews] Redis snapshot cache disabled: ping failed addr=%s err=%v", addr, err)
		_ = client.Close()
		disableNewsSnapshotRedis()
		return
	}

	newsSnapshotRedisState.mu.Lock()
	old := newsSnapshotRedisState.client
	newsSnapshotRedisState.client = client
	newsSnapshotRedisState.key = key
	newsSnapshotRedisState.ttl = ttl
	newsSnapshotRedisState.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	// 初始化成功后允许下一次启动流程执行快照回填。
	newsSnapshotRedisLoadOnce = sync.Once{}
	log.Printf("[WsNews] Redis snapshot cache enabled addr=%s db=%d key=%s ttl=%s", addr, cfg.DB, key, ttl)
}

func disableNewsSnapshotRedis() {
	newsSnapshotRedisState.mu.Lock()
	old := newsSnapshotRedisState.client
	newsSnapshotRedisState.client = nil
	newsSnapshotRedisState.key = ""
	newsSnapshotRedisState.ttl = 0
	newsSnapshotRedisState.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func getNewsSnapshotRedisState() (*redis.Client, string, time.Duration) {
	newsSnapshotRedisState.mu.RLock()
	defer newsSnapshotRedisState.mu.RUnlock()
	return newsSnapshotRedisState.client, newsSnapshotRedisState.key, newsSnapshotRedisState.ttl
}

// preloadNewsSnapshotFromRedis 尝试回填新闻快照（仅执行一次）。
func preloadNewsSnapshotFromRedis() {
	newsSnapshotRedisLoadOnce.Do(func() {
		ok, err := loadNewsSnapshotFromRedis()
		if err != nil {
			log.Printf("[WsNews] Redis preload failed: %v", err)
			return
		}
		if ok {
			log.Printf("[WsNews] Redis preload loaded snapshot")
		}
	})
}

func loadNewsSnapshotFromRedis() (bool, error) {
	client, key, _ := getNewsSnapshotRedisState()
	if client == nil || key == "" {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var payload newsSnapshotRedisPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false, err
	}
	if len(payload.Data) == 0 {
		return false, nil
	}

	updatedAt := payload.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = time.Now().UnixMilli()
	}
	storeNewsSnapshot(payload.Data, updatedAt)
	setNewsHubLastMessage(payload.Data, updatedAt)
	return true, nil
}

// persistNewsSnapshotToRedis 异步持久化新闻快照。
func persistNewsSnapshotToRedis(data map[string][]newsItem, updatedAt int64) {
	client, key, ttl := getNewsSnapshotRedisState()
	if client == nil || key == "" || len(data) == 0 {
		return
	}

	payload := newsSnapshotRedisPayload{
		UpdatedAt: updatedAt,
		Data:      data,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[WsNews] Redis persist marshal failed: %v", err)
		return
	}

	go func(c *redis.Client, redisKey string, expire time.Duration, body []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := c.Set(ctx, redisKey, body, expire).Err(); err != nil {
			log.Printf("[WsNews] Redis persist failed key=%s err=%v", redisKey, err)
		}
	}(client, key, ttl, raw)
}

func setNewsHubLastMessage(data map[string][]newsItem, updatedAt int64) {
	if len(data) == 0 {
		return
	}

	raw, err := json.Marshal(newsPayload{
		Channel: "news",
		Data:    data,
		Time:    updatedAt,
	})
	if err != nil {
		return
	}

	nHub.mu.Lock()
	nHub.lastMsg = raw
	nHub.mu.Unlock()
}
