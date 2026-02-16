package api

import (
	"fmt"
	"log"
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

	// 自动迁移表结构
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	log.Printf("[DB] Connected to PostgreSQL: %s:%d/%s", Cfg.Database.Host, Cfg.Database.Port, Cfg.Database.DBName)
	return nil
}

// autoMigrate 自动创建/更新表结构
func autoMigrate() error {
	return DB.AutoMigrate(
		&TradeRecord{},
	)
}

// ========== 数据模型 ==========

// TradeRecord 交易记录
type TradeRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Symbol         string    `gorm:"type:varchar(20);index" json:"symbol"`
	Side           string    `gorm:"type:varchar(10)" json:"side"`                 // BUY / SELL
	PositionSide   string    `gorm:"type:varchar(10)" json:"positionSide"`         // LONG / SHORT / BOTH
	OrderType      string    `gorm:"type:varchar(20)" json:"orderType"`            // MARKET / LIMIT
	OrderID        int64     `gorm:"index" json:"orderId"`
	Quantity       string    `gorm:"type:varchar(30)" json:"quantity"`
	Price          string    `gorm:"type:varchar(30)" json:"price"`                // 成交均价
	QuoteQuantity  string    `gorm:"type:varchar(30)" json:"quoteQuantity"`        // 下单金额 (USDT)
	Leverage       int       `json:"leverage"`
	StopLossPrice  string    `gorm:"type:varchar(30)" json:"stopLossPrice,omitempty"`
	TakeProfitPrice string  `gorm:"type:varchar(30)" json:"takeProfitPrice,omitempty"`
	StopLossAlgoID  int64   `json:"stopLossAlgoId,omitempty"`
	TakeProfitAlgoID int64  `json:"takeProfitAlgoId,omitempty"`
	RealizedPnl    string    `gorm:"type:varchar(30)" json:"realizedPnl,omitempty"` // 已实现盈亏
	Status         string    `gorm:"type:varchar(20);index" json:"status"`          // OPEN / CLOSED
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// ========== 数据库操作 ==========

// SaveTradeRecord 保存交易记录
func SaveTradeRecord(record *TradeRecord) error {
	if DB == nil {
		return nil
	}
	return DB.Create(record).Error
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
