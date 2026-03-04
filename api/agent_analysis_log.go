package api

import (
	"strings"
	"time"
)

const (
	AgentAnalysisStatusPending = "PENDING"
	AgentAnalysisStatusRunning = "RUNNING"
	AgentAnalysisStatusSuccess = "SUCCESS"
	AgentAnalysisStatusFailed  = "FAILED"

	AgentAnalysisSourceAppManual = "APP_MANUAL"
	AgentAnalysisSourceDailyAuto = "DAILY_AUTO"
)

// AgentAnalysisLog Agent 分析请求/结果日志。
type AgentAnalysisLog struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Mode          string    `gorm:"type:varchar(20);index" json:"mode"`
	Source        string    `gorm:"type:varchar(20);index" json:"source"` // APP_MANUAL / DAILY_AUTO
	Symbols       string    `gorm:"type:text" json:"symbols"`
	Execute       bool      `gorm:"index" json:"execute"`
	Status        string    `gorm:"type:varchar(20);index" json:"status"` // PENDING / RUNNING / SUCCESS / FAILED
	ErrorMessage  string    `gorm:"type:text" json:"errorMessage,omitempty"`
	DurationMs    int64     `json:"durationMs"`
	RequestBody   string    `gorm:"type:text" json:"requestBody,omitempty"`
	ResponseBody  string    `gorm:"type:text" json:"responseBody,omitempty"`
	ExecutionBody string    `gorm:"type:text" json:"executionBody,omitempty"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

// SaveAgentAnalysisLog 保存 Agent 分析日志。
func SaveAgentAnalysisLog(record *AgentAnalysisLog) error {
	if DB == nil || record == nil {
		return nil
	}
	if record.Status == "" {
		record.Status = AgentAnalysisStatusSuccess
	} else {
		record.Status = normalizeAgentAnalysisStatus(record.Status)
	}
	if record.Source == "" {
		record.Source = AgentAnalysisSourceAppManual
	} else {
		record.Source = normalizeAgentAnalysisSource(record.Source)
	}
	return DB.Create(record).Error
}

// GetAgentAnalysisLogs 查询 Agent 分析日志（按创建时间倒序）。
func GetAgentAnalysisLogs(limit int, status string, execute *bool) ([]AgentAnalysisLog, error) {
	if DB == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var records []AgentAnalysisLog
	q := DB.Order("created_at DESC").Limit(limit)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if execute != nil {
		q = q.Where("execute = ?", *execute)
	}
	if err := q.Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// GetAgentAnalysisLogByID 按 ID 查询单条 Agent 分析日志。
func GetAgentAnalysisLogByID(id uint) (*AgentAnalysisLog, error) {
	if DB == nil || id == 0 {
		return nil, nil
	}
	var record AgentAnalysisLog
	if err := DB.First(&record, id).Error; err != nil {
		return nil, err
	}
	return &record, nil
}

// UpdateAgentAnalysisLog 按 ID 更新 Agent 分析日志。
func UpdateAgentAnalysisLog(id uint, updates map[string]any) error {
	if DB == nil || id == 0 || len(updates) == 0 {
		return nil
	}
	if status, ok := updates["status"].(string); ok {
		updates["status"] = normalizeAgentAnalysisStatus(status)
	}
	if source, ok := updates["source"].(string); ok {
		updates["source"] = normalizeAgentAnalysisSource(source)
	}
	return DB.Model(&AgentAnalysisLog{}).Where("id = ?", id).Updates(updates).Error
}

func normalizeAgentAnalysisStatus(status string) string {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case AgentAnalysisStatusPending, AgentAnalysisStatusRunning, AgentAnalysisStatusSuccess, AgentAnalysisStatusFailed:
		return status
	default:
		return AgentAnalysisStatusFailed
	}
}

func normalizeAgentAnalysisSource(source string) string {
	source = strings.ToUpper(strings.TrimSpace(source))
	switch source {
	case AgentAnalysisSourceAppManual, AgentAnalysisSourceDailyAuto:
		return source
	default:
		return AgentAnalysisSourceAppManual
	}
}
