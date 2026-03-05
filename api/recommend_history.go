package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/redis/go-redis/v9"
)

const (
	recommendHistoryRedisKeySuffix = "recommend:signal:history:v1"
	recommendHistoryRedisMaxPerDay = 3000
	recommendHistoryRedisExpire    = 48 * time.Hour
)

type recommendSignalHistoryItem struct {
	ID         uint       `json:"id,omitempty"`
	Symbol     string     `json:"symbol"`
	Direction  string     `json:"direction"`
	Confidence int        `json:"confidence"`
	Entry      float64    `json:"entry"`
	StopLoss   float64    `json:"stopLoss"`
	TakeProfit float64    `json:"takeProfit"`
	Reasons    []string   `json:"reasons"`
	Signals    []tfSignal `json:"signals"`
	Source     string     `json:"source"`
	ScannedAt  time.Time  `json:"scannedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

var recommendHistoryRedisState struct {
	mu        sync.RWMutex
	client    *redis.Client
	keyPrefix string
}

func InitRecommendSignalHistoryRedis(cfg RedisConfig) {
	if !cfg.Enabled {
		disableRecommendSignalHistoryRedis()
		return
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		log.Printf("[RecommendHistory] Redis disabled: empty addr")
		disableRecommendSignalHistoryRedis()
		return
	}

	keyPrefix := strings.TrimSpace(cfg.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = "tool:"
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
		log.Printf("[RecommendHistory] Redis disabled: ping failed addr=%s err=%v", addr, err)
		_ = client.Close()
		disableRecommendSignalHistoryRedis()
		return
	}

	recommendHistoryRedisState.mu.Lock()
	old := recommendHistoryRedisState.client
	recommendHistoryRedisState.client = client
	recommendHistoryRedisState.keyPrefix = keyPrefix
	recommendHistoryRedisState.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	log.Printf("[RecommendHistory] Redis enabled addr=%s db=%d keyPrefix=%s", addr, cfg.DB, keyPrefix)
}

func disableRecommendSignalHistoryRedis() {
	recommendHistoryRedisState.mu.Lock()
	old := recommendHistoryRedisState.client
	recommendHistoryRedisState.client = nil
	recommendHistoryRedisState.keyPrefix = ""
	recommendHistoryRedisState.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func getRecommendHistoryRedis() (*redis.Client, string) {
	recommendHistoryRedisState.mu.RLock()
	defer recommendHistoryRedisState.mu.RUnlock()
	return recommendHistoryRedisState.client, recommendHistoryRedisState.keyPrefix
}

func recommendHistoryRedisKey(day time.Time, keyPrefix string) string {
	if day.IsZero() {
		day = time.Now()
	}
	return fmt.Sprintf("%s%s:%s", keyPrefix, recommendHistoryRedisKeySuffix, day.Format("20060102"))
}

func saveRecommendSignalRecord(record *RecommendSignalRecord) error {
	if DB == nil || record == nil {
		return nil
	}
	return DB.Create(record).Error
}

func getRecommendSignalRecords(symbol, direction string, limit int) ([]RecommendSignalRecord, error) {
	if DB == nil {
		return nil, nil
	}
	q := DB.Order("scanned_at DESC").Order("id DESC")
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	if direction != "" {
		q = q.Where("direction = ?", direction)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var records []RecommendSignalRecord
	if err := q.Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func persistRecommendSignalHistoryBatch(items []RecommendItem, scannedAt time.Time, source string) {
	if len(items) == 0 {
		return
	}
	if scannedAt.IsZero() {
		scannedAt = time.Now()
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "recommend"
	}

	copied := make([]RecommendItem, len(items))
	copy(copied, items)

	go func(list []RecommendItem, at time.Time, src string) {
		for _, item := range list {
			saveRecommendSignalHistory(item, at, src)
		}
	}(copied, scannedAt, source)
}

func saveRecommendSignalHistory(item RecommendItem, scannedAt time.Time, source string) {
	symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
	direction := strings.ToUpper(strings.TrimSpace(item.Direction))
	if symbol == "" || (direction != "LONG" && direction != "SHORT") {
		return
	}
	if scannedAt.IsZero() {
		scannedAt = time.Now()
	}

	reasonsRaw, err := json.Marshal(item.Reasons)
	if err != nil {
		reasonsRaw = []byte("[]")
	}
	signalsRaw, err := json.Marshal(item.Signals)
	if err != nil {
		signalsRaw = []byte("[]")
	}

	record := &RecommendSignalRecord{
		Symbol:     symbol,
		Direction:  direction,
		Confidence: item.Confidence,
		Entry:      item.Entry,
		StopLoss:   item.StopLoss,
		TakeProfit: item.TakeProfit,
		Reasons:    string(reasonsRaw),
		Signals:    string(signalsRaw),
		Source:     source,
		ScannedAt:  scannedAt,
	}
	if err := saveRecommendSignalRecord(record); err != nil {
		log.Printf("[RecommendHistory] DB save failed symbol=%s source=%s err=%v", symbol, source, err)
	}

	cacheItem := recommendSignalHistoryItem{
		ID:         record.ID,
		Symbol:     symbol,
		Direction:  direction,
		Confidence: item.Confidence,
		Entry:      item.Entry,
		StopLoss:   item.StopLoss,
		TakeProfit: item.TakeProfit,
		Reasons:    append([]string(nil), item.Reasons...),
		Signals:    append([]tfSignal(nil), item.Signals...),
		Source:     source,
		ScannedAt:  scannedAt,
		CreatedAt:  time.Now(),
	}
	appendRecommendSignalHistoryToRedis(cacheItem)
}

func appendRecommendSignalHistoryToRedis(item recommendSignalHistoryItem) {
	client, keyPrefix := getRecommendHistoryRedis()
	if client == nil || keyPrefix == "" {
		return
	}
	if item.ScannedAt.IsZero() {
		item.ScannedAt = time.Now()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}

	key := recommendHistoryRedisKey(item.ScannedAt, keyPrefix)
	raw, err := json.Marshal(item)
	if err != nil {
		log.Printf("[RecommendHistory] Redis marshal failed symbol=%s err=%v", item.Symbol, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pipe := client.TxPipeline()
	pipe.LPush(ctx, key, raw)
	pipe.LTrim(ctx, key, 0, recommendHistoryRedisMaxPerDay-1)
	pipe.Expire(ctx, key, recommendHistoryRedisExpire)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[RecommendHistory] Redis append failed key=%s err=%v", key, err)
	}
}

func loadRecommendSignalHistoryFromRedis(day time.Time, symbol, direction string, limit int) ([]recommendSignalHistoryItem, error) {
	client, keyPrefix := getRecommendHistoryRedis()
	if client == nil || keyPrefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	key := recommendHistoryRedisKey(day, keyPrefix)
	fetchN := int64(limit * 4)
	if fetchN < int64(limit) {
		fetchN = int64(limit)
	}
	if fetchN > recommendHistoryRedisMaxPerDay {
		fetchN = recommendHistoryRedisMaxPerDay
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := client.LRange(ctx, key, 0, fetchN-1).Result()
	if err == redis.Nil {
		return []recommendSignalHistoryItem{}, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]recommendSignalHistoryItem, 0, len(rows))
	for _, row := range rows {
		var item recommendSignalHistoryItem
		if err := json.Unmarshal([]byte(row), &item); err != nil {
			continue
		}
		if symbol != "" && item.Symbol != symbol {
			continue
		}
		if direction != "" && item.Direction != direction {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].ScannedAt.Equal(out[j].ScannedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ScannedAt.After(out[j].ScannedAt)
	})
	return out, nil
}

func parseRecommendHistoryItemsFromDB(records []RecommendSignalRecord) []recommendSignalHistoryItem {
	out := make([]recommendSignalHistoryItem, 0, len(records))
	for _, rec := range records {
		item := recommendSignalHistoryItem{
			ID:         rec.ID,
			Symbol:     rec.Symbol,
			Direction:  rec.Direction,
			Confidence: rec.Confidence,
			Entry:      rec.Entry,
			StopLoss:   rec.StopLoss,
			TakeProfit: rec.TakeProfit,
			Source:     rec.Source,
			ScannedAt:  rec.ScannedAt,
			CreatedAt:  rec.CreatedAt,
		}
		if err := json.Unmarshal([]byte(rec.Reasons), &item.Reasons); err != nil {
			item.Reasons = []string{}
		}
		if err := json.Unmarshal([]byte(rec.Signals), &item.Signals); err != nil {
			item.Signals = []tfSignal{}
		}
		out = append(out, item)
	}
	return out
}

func getRecommendSignalHistory(symbol, direction string, limit int) ([]recommendSignalHistoryItem, string, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	direction = strings.ToUpper(strings.TrimSpace(direction))
	if direction != "LONG" && direction != "SHORT" {
		direction = ""
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	if DB != nil {
		records, err := getRecommendSignalRecords(symbol, direction, limit)
		if err != nil {
			return nil, "", err
		}
		return parseRecommendHistoryItemsFromDB(records), "db", nil
	}

	items, err := loadRecommendSignalHistoryFromRedis(time.Now(), symbol, direction, limit)
	if err != nil {
		return nil, "", err
	}
	return items, "redis", nil
}

// HandleRecommendHistory GET /tool/recommend/history?symbol=BTCUSDT&direction=LONG&limit=100
func HandleRecommendHistory(c context.Context, ctx *app.RequestContext) {
	symbol := ctx.DefaultQuery("symbol", "")
	direction := ctx.DefaultQuery("direction", "")
	limitStr := ctx.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 100
	}

	items, source, err := getRecommendSignalHistory(symbol, direction, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{"error": "获取推荐历史失败: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, utils.H{"data": utils.H{
		"items":  items,
		"count":  len(items),
		"source": source,
	}})
}
