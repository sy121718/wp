package aimodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameAIToolIdempotency = "ai_tool_idempotency"

const idempotencyWindow = 24 * time.Hour

type ToolIdempotencyRow struct {
	ToolName   string    `gorm:"column:tool_name"`
	IdemKey    string    `gorm:"column:idem_key"`
	ResultText string    `gorm:"column:result_text"`
	CreateTime time.Time `gorm:"column:create_time"`
}

type ToolIdempotencyModel struct{ db *gorm.DB }

func NewToolIdempotencyModel(db *gorm.DB) *ToolIdempotencyModel {
	return &ToolIdempotencyModel{db: db}
}

func (m *ToolIdempotencyModel) Lookup(ctx context.Context, tool string, actor int64, key string) (row ToolIdempotencyRow, ok bool, err error) {

	err = m.db.WithContext(ctx).Table(tableNameAIToolIdempotency).
		Select("tool_name, idem_key, result_text, create_time").
		Where("tool_name = ? AND actor = ? AND idem_key = ? AND create_time > now() - ?::interval",
			tool, actor, key, idempotencyWindow.String()).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return ToolIdempotencyRow{}, false, err
	}
	if row.ToolName == "" {
		return ToolIdempotencyRow{}, false, nil
	}
	return row, true, nil
}

func (m *ToolIdempotencyModel) Save(ctx context.Context, tool string, actor int64, key, resultText string) error {

	return m.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Table(tableNameAIToolIdempotency).
		Create(map[string]any{
			"tool_name":   tool,
			"actor":       actor,
			"idem_key":    key,
			"result_text": resultText,
		}).Error
}
