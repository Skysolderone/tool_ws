package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 全局数据库实例
var DB *gorm.DB

// InitDB 初始化 PostgreSQL 数据库连接
func InitDB() error {
	dsn := Cfg.Database.DSN()
	if dsn == "" || Cfg.Database.Host == "" {
		log.Println("[DB] No database config, skipping DB init")
		return nil
	}

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	// 配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	log.Printf("[DB] Connected to PostgreSQL: %s:%d/%s", Cfg.Database.Host, Cfg.Database.Port, Cfg.Database.DBName)
	return nil
}

// RunMigrations 显式执行数据库迁移（表结构 + 索引）
func RunMigrations() error {
	if DB == nil {
		return nil
	}
	if err := autoMigrateSchema(); err != nil {
		return err
	}
	if err := ensureNumericScale8(); err != nil {
		return err
	}
	if err := ensureDBIndexes(); err != nil {
		return err
	}
	return nil
}

// autoMigrateSchema 自动创建/更新表结构
func autoMigrateSchema() error {
	return DB.AutoMigrate(
		&TradeRecord{},
		&OperationRecord{},
		&RecommendSignalRecord{},
		&RecommendSignalEvaluationRecord{},
		&LiquidationStatRecord{},
		&LocalTPSLCondition{},
		&StrategyState{},
		&SlippageRecord{},
		&SlippageRecord{}, // 滑点记录（新增表，不影响已有数据）
		&VarSnapshot{},
		&StrategyAllocation{},
	)
}

func ensureDBIndexes() error {
	if err := createIndexIfMissing(&TradeRecord{}, "Source"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&TradeRecord{}, "Symbol"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&TradeRecord{}, "OrderID"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&TradeRecord{}, "CloseReason"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&TradeRecord{}, "ClosedAt"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&TradeRecord{}, "Status"); err != nil {
		return err
	}

	if err := createIndexIfMissing(&OperationRecord{}, "Symbol"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&OperationRecord{}, "Source"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&OperationRecord{}, "Action"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&OperationRecord{}, "Status"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&OperationRecord{}, "RelatedOrderID"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalRecord{}, "Symbol"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalRecord{}, "Direction"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalRecord{}, "Source"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalRecord{}, "ScannedAt"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "SignalRecordID"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Symbol"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Direction"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Source"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "ScannedAt"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Evaluated1H"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Evaluated4H"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Evaluated24H"); err != nil {
		return err
	}
	if err := createIndexIfMissing(&RecommendSignalEvaluationRecord{}, "Final"); err != nil {
		return err
	}
	return nil
}

// ensureNumericScale8 将历史 numeric(36,18) 列迁移到 numeric(36,8)。
// 仅在 scale 非 8 时执行 ALTER，避免每次启动都触发表锁。
func ensureNumericScale8() error {
	if DB == nil {
		return nil
	}

	targetCols := []struct {
		table  string
		column string
	}{
		{"trade_records", "quantity"},
		{"trade_records", "price"},
		{"trade_records", "quote_quantity"},
		{"trade_records", "stop_loss_price"},
		{"trade_records", "take_profit_price"},
		{"trade_records", "realized_pnl"},
		{"local_tpsl_conditions", "trigger_price"},
		{"local_tpsl_conditions", "entry_price"},
		{"local_tpsl_conditions", "trailing_activation_price"},
		{"local_tpsl_conditions", "trailing_highest_price"},
		{"slippage_records", "intended_price"},
		{"slippage_records", "executed_price"},
		{"slippage_records", "quantity"},
		{"slippage_records", "arrival_price"},
		{"recommend_signal_evaluation_records", "entry_price"},
		{"recommend_signal_evaluation_records", "current_price"},
	}

	for _, item := range targetCols {
		var info struct {
			Cnt   int64 `gorm:"column:cnt"`
			Scale int64 `gorm:"column:scale"`
		}
		if err := DB.Raw(
			`SELECT COUNT(*) AS cnt, COALESCE(MAX(numeric_scale), -1) AS scale
			 FROM information_schema.columns
			 WHERE table_schema = current_schema()
			   AND table_name = ?
			   AND column_name = ?`,
			item.table, item.column,
		).Scan(&info).Error; err != nil {
			return fmt.Errorf("query numeric scale for %s.%s: %w", item.table, item.column, err)
		}
		if info.Cnt == 0 {
			log.Printf("[DB] Skip numeric scale migration, column missing: %s.%s", item.table, item.column)
			continue
		}
		if info.Scale == 8 {
			continue
		}

		sql := fmt.Sprintf(
			"ALTER TABLE %s ALTER COLUMN %s TYPE numeric(36,8) USING ROUND(%s::numeric, 8)",
			item.table, item.column, item.column,
		)
		if err := DB.Exec(sql).Error; err != nil {
			return fmt.Errorf("alter %s.%s to numeric(36,8): %w", item.table, item.column, err)
		}
		log.Printf("[DB] Migrated %s.%s numeric scale: %d -> 8", item.table, item.column, info.Scale)
	}

	return nil
}

func createIndexIfMissing(model any, field string) error {
	if DB.Migrator().HasIndex(model, field) {
		return nil
	}
	if err := DB.Migrator().CreateIndex(model, field); err != nil {
		return fmt.Errorf("create index %T.%s: %w", model, field, err)
	}
	return nil
}

// ========== 数据模型 ==========

// TradeRecord 交易记录
type TradeRecord struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	Source           string     `gorm:"type:varchar(40);index" json:"source"` // manual / strategy_xxx / hyper_follow
	Symbol           string     `gorm:"type:varchar(20);index" json:"symbol"`
	Side             string     `gorm:"type:varchar(10)" json:"side"`         // BUY / SELL
	PositionSide     string     `gorm:"type:varchar(10)" json:"positionSide"` // LONG / SHORT / BOTH
	OrderType        string     `gorm:"type:varchar(20)" json:"orderType"`    // MARKET / LIMIT
	OrderID          int64      `gorm:"index" json:"orderId"`
	Quantity         float64    `gorm:"type:numeric(36,8)" json:"quantity"`
	Price            float64    `gorm:"type:numeric(36,8)" json:"price"`         // 成交均价
	QuoteQuantity    float64    `gorm:"type:numeric(36,8)" json:"quoteQuantity"` // 下单金额 (USDT)
	Leverage         int        `json:"leverage"`
	StopLossPrice    *float64   `gorm:"type:numeric(36,8)" json:"stopLossPrice,omitempty"`
	TakeProfitPrice  *float64   `gorm:"type:numeric(36,8)" json:"takeProfitPrice,omitempty"`
	StopLossAlgoID   int64      `json:"stopLossAlgoId,omitempty"`
	TakeProfitAlgoID int64      `json:"takeProfitAlgoId,omitempty"`
	RealizedPnl      float64    `gorm:"type:numeric(36,8)" json:"realizedPnl"` // 已实现盈亏
	CloseReason      string     `gorm:"type:varchar(40);index" json:"closeReason,omitempty"`
	ClosedAt         *time.Time `gorm:"index" json:"closedAt,omitempty"`
	Status           string     `gorm:"type:varchar(20);index" json:"status"` // OPEN / CLOSED
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime" json:"updatedAt"`
}

// OperationRecord 操作记录（用于追踪失败下单等事件）
type OperationRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Symbol         string    `gorm:"type:varchar(20);index" json:"symbol"`
	Source         string    `gorm:"type:varchar(40);index" json:"source"` // manual / strategy_xxx / unknown
	Action         string    `gorm:"type:varchar(40);index" json:"action"` // PLACE_ORDER / PLACE_TPSL
	Status         string    `gorm:"type:varchar(20);index" json:"status"` // FAILED / SUCCESS
	ErrorMessage   string    `gorm:"type:text" json:"errorMessage"`
	RequestBody    string    `gorm:"type:text" json:"requestBody,omitempty"`
	RelatedOrderID int64     `gorm:"index" json:"relatedOrderId,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// RecommendSignalRecord 推荐信号历史记录（数据库永久保存）。
type RecommendSignalRecord struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Symbol     string    `gorm:"type:varchar(20);index" json:"symbol"`
	Direction  string    `gorm:"type:varchar(10);index" json:"direction"` // LONG / SHORT
	Confidence int       `json:"confidence"`
	Entry      float64   `gorm:"type:numeric(36,8)" json:"entry"`
	StopLoss   float64   `gorm:"type:numeric(36,8)" json:"stopLoss"`
	TakeProfit float64   `gorm:"type:numeric(36,8)" json:"takeProfit"`
	Reasons    string    `gorm:"type:text" json:"reasons"` // JSON string
	Signals    string    `gorm:"type:text" json:"signals"` // JSON string
	Source     string    `gorm:"type:varchar(30);index" json:"source"`
	ScannedAt  time.Time `gorm:"index" json:"scannedAt"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// ========== 数据库操作 ==========

// SaveTradeRecord 保存交易记录
func SaveTradeRecord(record *TradeRecord) error {
	if DB == nil || record == nil {
		return nil
	}
	if record.Source == "" {
		record.Source = "manual"
	}
	if record.Status == "" {
		record.Status = "OPEN"
	}
	if record.Status == "CLOSED" && record.ClosedAt == nil {
		now := time.Now().UTC()
		record.ClosedAt = &now
	}

	if record.OrderID != 0 {
		var existing TradeRecord
		err := DB.Where("order_id = ?", record.OrderID).First(&existing).Error
		if err == nil {
			mergeTradeRecord(&existing, record)
			return DB.Save(&existing).Error
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	return DB.Create(record).Error
}

func mergeTradeRecord(dst *TradeRecord, src *TradeRecord) {
	if dst == nil || src == nil {
		return
	}
	if dst.Source == "" && src.Source != "" {
		dst.Source = src.Source
	}
	if dst.Symbol == "" && src.Symbol != "" {
		dst.Symbol = src.Symbol
	}
	if dst.Side == "" && src.Side != "" {
		dst.Side = src.Side
	}
	if dst.PositionSide == "" && src.PositionSide != "" {
		dst.PositionSide = src.PositionSide
	}
	if dst.OrderType == "" && src.OrderType != "" {
		dst.OrderType = src.OrderType
	}
	if src.Quantity > 0 {
		dst.Quantity = src.Quantity
	}
	if src.Price > 0 {
		dst.Price = src.Price
	}
	if src.QuoteQuantity > 0 {
		dst.QuoteQuantity = src.QuoteQuantity
	}
	if src.Leverage > 0 {
		dst.Leverage = src.Leverage
	}
	if src.StopLossPrice != nil {
		dst.StopLossPrice = src.StopLossPrice
	}
	if src.TakeProfitPrice != nil {
		dst.TakeProfitPrice = src.TakeProfitPrice
	}
	if src.StopLossAlgoID != 0 {
		dst.StopLossAlgoID = src.StopLossAlgoID
	}
	if src.TakeProfitAlgoID != 0 {
		dst.TakeProfitAlgoID = src.TakeProfitAlgoID
	}
	if src.RealizedPnl != 0 {
		dst.RealizedPnl = src.RealizedPnl
	}
	if src.CloseReason != "" {
		dst.CloseReason = src.CloseReason
	}
	if src.ClosedAt != nil {
		dst.ClosedAt = src.ClosedAt
	}
	if src.Status != "" {
		// 不允许被重复写入回滚终态（例如 CLOSED/CANCELED 被 OPEN 覆盖）。
		if src.Status == "CLOSED" || src.Status == "CANCELED" || dst.Status == "" || dst.Status == "OPEN" {
			dst.Status = src.Status
		}
	}
}

// SaveOperationRecord 保存操作记录
func SaveOperationRecord(record *OperationRecord) error {
	if DB == nil || record == nil {
		return nil
	}
	return DB.Create(record).Error
}

// SaveFailedOperation 保存失败操作记录
func SaveFailedOperation(action, source, symbol string, req any, relatedOrderID int64, opErr error) {
	if opErr == nil {
		return
	}
	_ = saveOperation(action, source, symbol, req, relatedOrderID, "FAILED", opErr.Error())
}

// SaveSuccessOperation 保存成功操作记录
func SaveSuccessOperation(action, source, symbol string, req any, relatedOrderID int64) {
	_ = saveOperation(action, source, symbol, req, relatedOrderID, "SUCCESS", "")
}

func saveOperation(action, source, symbol string, req any, relatedOrderID int64, status, errMsg string) error {
	if source == "" {
		source = "unknown"
	}

	reqBody := ""
	if req != nil {
		b, err := json.Marshal(req)
		if err != nil {
			reqBody = fmt.Sprintf(`{"marshalError":%q}`, err.Error())
		} else {
			reqBody = string(b)
		}
	}

	record := &OperationRecord{
		Symbol:         symbol,
		Source:         source,
		Action:         action,
		Status:         status,
		ErrorMessage:   errMsg,
		RequestBody:    reqBody,
		RelatedOrderID: relatedOrderID,
	}
	if err := SaveOperationRecord(record); err != nil {
		log.Printf("[DB] Failed to save operation record: %v", err)
		return err
	}
	return nil
}

// UpdateTradeRecord 更新交易记录
func UpdateTradeRecord(record *TradeRecord) error {
	if DB == nil {
		return nil
	}
	return DB.Save(record).Error
}

// GetTradeRecords 查询交易记录
func GetTradeRecords(symbol string, limit int) ([]TradeRecord, error) {
	if DB == nil {
		return nil, nil
	}
	var records []TradeRecord
	q := DB.Order("created_at DESC")
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&records).Error
	return records, err
}

// GetOperationRecords 查询操作记录
func GetOperationRecords(symbol, status string, limit int) ([]OperationRecord, error) {
	if DB == nil {
		return nil, nil
	}
	var records []OperationRecord
	q := DB.Order("created_at DESC")
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&records).Error
	return records, err
}

// GetTradeByOrderID 根据订单 ID 查询
func GetTradeByOrderID(orderID int64) (*TradeRecord, error) {
	if DB == nil {
		return nil, nil
	}
	var record TradeRecord
	err := DB.Where("order_id = ?", orderID).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func parseNumeric(v string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return n
}

func parseNumericPtr(v string) *float64 {
	n := parseNumeric(v)
	if n == 0 {
		return nil
	}
	return &n
}
