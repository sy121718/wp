package aimodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameAIToolCallLog = "ai_tool_call_log"

type AIToolCallLogEntity struct {
	ID int64 `gorm:"column:id;primaryKey"`

	SessionID int64 `gorm:"column:session_id"`
	UserID    int64 `gorm:"column:user_id"`

	ToolName string `gorm:"column:tool_name"`

	ArgumentsSummary string `gorm:"column:arguments_summary"`
	ResultSummary    string `gorm:"column:result_summary"`

	ResultLen int64 `gorm:"column:result_len"`

	Truncated bool `gorm:"column:truncated"`

	Status string `gorm:"column:status"`

	ErrorKey string `gorm:"column:error_key"`

	LatencyMs  int64     `gorm:"column:latency_ms"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (AIToolCallLogEntity) TableName() string { return tableNameAIToolCallLog }

type ToolCallLogModel struct {
	db *gorm.DB
}

func NewToolCallLogModel(db *gorm.DB) *ToolCallLogModel { return &ToolCallLogModel{db: db} }

func (m *ToolCallLogModel) logs(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIToolCallLogEntity{})
}

func (m *ToolCallLogModel) Insert(ctx context.Context, e *AIToolCallLogEntity) error {
	if e == nil {
		return nil
	}
	return m.logs(ctx).Create(e).Error
}

func (m *ToolCallLogModel) ListBySession(ctx context.Context, sessionID int64, limit int) (rows []AIToolCallLogEntity, err error) {
	if limit <= 0 {
		limit = 50
	}
	err = m.logs(ctx).Where("session_id = ?", sessionID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
