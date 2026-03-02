package api

import "time"

// AgentAnalysisLog Agent 分析请求/结果日志。
type AgentAnalysisLog struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Mode          string    `gorm:"type:varchar(20);index" json:"mode"`
	Symbols       string    `gorm:"type:text" json:"symbols"`
	Execute       bool      `gorm:"index" json:"execute"`
	Status        string    `gorm:"type:varchar(20);index" json:"status"` // SUCCESS / FAILED
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
		record.Status = "SUCCESS"
	}
	return DB.Create(record).Error
}
