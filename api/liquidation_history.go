package api

import (
	"time"

	"gorm.io/gorm/clause"
)

const (
	liquidationIntervalDay = "1d"
	liquidationIntervalH4  = "4h"
	liquidationIntervalH1  = "1h"
)

// LiquidationStatRecord 强平统计历史（按时间桶 upsert）
type LiquidationStatRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	BucketInterval string    `gorm:"column:bucket_interval;type:varchar(8);index:idx_liq_bucket_start,unique;index:idx_liq_interval_created" json:"bucketInterval"`
	StartTime      int64     `gorm:"column:start_time;index:idx_liq_bucket_start,unique;index" json:"startTime"`
	EndTime        int64     `gorm:"column:end_time" json:"endTime"`
	TotalCount     int64     `gorm:"column:total_count" json:"totalCount"`
	BuyCount       int64     `gorm:"column:buy_count" json:"buyCount"`
	SellCount      int64     `gorm:"column:sell_count" json:"sellCount"`
	TotalNotional  float64   `gorm:"column:total_notional" json:"totalNotional"`
	BuyNotional    float64   `gorm:"column:buy_notional" json:"buyNotional"`
	SellNotional   float64   `gorm:"column:sell_notional" json:"sellNotional"`
	SnapshotTime   int64     `gorm:"column:snapshot_time;index" json:"snapshotTime"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

func (LiquidationStatRecord) TableName() string {
	return "liquidation_stats_history"
}

type LiquidationHistoryData struct {
	Daily []LiquidationStatRecord `json:"daily"`
	H4    []LiquidationStatRecord `json:"h4"`
	H1    []LiquidationStatRecord `json:"h1"`
}

// SaveLiquidationSnapshot 保存一次统计刷新（仅保存每个粒度的当前桶，避免写放大）
func SaveLiquidationSnapshot(payload liquidationStatsPayload) error {
	if DB == nil {
		return nil
	}

	rows := make([]LiquidationStatRecord, 0, 3)
	appendCurrent := func(interval string, list []liquidationBucketStat) {
		if len(list) == 0 {
			return
		}
		b := list[0]
		rows = append(rows, LiquidationStatRecord{
			BucketInterval: interval,
			StartTime:      b.StartTime,
			EndTime:        b.EndTime,
			TotalCount:     b.TotalCount,
			BuyCount:       b.BuyCount,
			SellCount:      b.SellCount,
			TotalNotional:  b.TotalNotional,
			BuyNotional:    b.BuyNotional,
			SellNotional:   b.SellNotional,
			SnapshotTime:   payload.Time,
		})
	}

	appendCurrent(liquidationIntervalDay, payload.Stats.Daily)
	appendCurrent(liquidationIntervalH4, payload.Stats.H4)
	appendCurrent(liquidationIntervalH1, payload.Stats.H1)
	if len(rows) == 0 {
		return nil
	}

	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "bucket_interval"},
			{Name: "start_time"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"end_time",
			"total_count",
			"buy_count",
			"sell_count",
			"total_notional",
			"buy_notional",
			"sell_notional",
			"snapshot_time",
			"updated_at",
		}),
	}).Create(&rows).Error
}

func GetLiquidationHistory(limit int) (LiquidationHistoryData, error) {
	data := LiquidationHistoryData{
		Daily: []LiquidationStatRecord{},
		H4:    []LiquidationStatRecord{},
		H1:    []LiquidationStatRecord{},
	}
	if DB == nil {
		return data, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	if err := DB.
		Where("bucket_interval = ?", liquidationIntervalDay).
		Order("start_time DESC").
		Limit(limit).
		Find(&data.Daily).Error; err != nil {
		return data, err
	}
	if err := DB.
		Where("bucket_interval = ?", liquidationIntervalH4).
		Order("start_time DESC").
		Limit(limit).
		Find(&data.H4).Error; err != nil {
		return data, err
	}
	if err := DB.
		Where("bucket_interval = ?", liquidationIntervalH1).
		Order("start_time DESC").
		Limit(limit).
		Find(&data.H1).Error; err != nil {
		return data, err
	}

	return data, nil
}
