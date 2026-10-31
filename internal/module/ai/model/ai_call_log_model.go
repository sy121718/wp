package aimodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameAICallLog = "ai_call_log"

type AICallLogEntity struct {
	ID int64 `gorm:"column:id;primaryKey"`

	SessionID int64 `gorm:"column:session_id"`
	UserID    int64 `gorm:"column:user_id"`

	ProviderKey string `gorm:"column:provider_key"`
	ModelID     string `gorm:"column:model_id"`
	Protocol    string `gorm:"column:protocol"`

	InputTokens  int64 `gorm:"column:input_tokens"`
	OutputTokens int64 `gorm:"column:output_tokens"`
	TotalTokens  int64 `gorm:"column:total_tokens"`

	CachedTokens   int64 `gorm:"column:cached_tokens"`
	CachedReported bool  `gorm:"column:cached_reported"`
	UsageReported  bool  `gorm:"column:usage_reported"`

	LatencyMs int64 `gorm:"column:latency_ms"`

	Status     string    `gorm:"column:status"`
	ErrorKey   string    `gorm:"column:error_key"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (AICallLogEntity) TableName() string { return tableNameAICallLog }

type CallLogModel struct {
	db *gorm.DB
}

func NewCallLogModel(db *gorm.DB) *CallLogModel { return &CallLogModel{db: db} }

func (m *CallLogModel) logs(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AICallLogEntity{})
}

func (m *CallLogModel) Insert(ctx context.Context, e *AICallLogEntity) error {
	if e == nil {
		return nil
	}
	return m.logs(ctx).Create(e).Error
}

const defaultSessionCallLimit = 5

type SessionCallLogRow struct {
	AICallLogEntity

	Total int64 `gorm:"column:total"`

	Rank int64 `gorm:"column:rank"`
}

func (m *CallLogModel) RecentCallsBySession(ctx context.Context, sessionIDs []int64, perSession int) (rows []SessionCallLogRow, err error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	if perSession <= 0 {
		perSession = defaultSessionCallLimit
	}

	inner := m.db.WithContext(ctx).
		Table(tableNameAICallLog+" AS c").
		Select("c.*, COUNT(*) OVER (PARTITION BY c.session_id) AS total, "+
			"ROW_NUMBER() OVER (PARTITION BY c.session_id ORDER BY c.id DESC) AS rn").
		Where("c.session_id IN ?", sessionIDs)

	err = m.db.WithContext(ctx).
		Table("(?) AS t", inner).
		Where("t.rn <= ?", perSession).
		Order("t.session_id ASC, t.rn ASC").
		Scan(&rows).Error
	return rows, err
}

func (m *CallLogModel) ListBySession(ctx context.Context, sessionID int64, limit int) (rows []AICallLogEntity, err error) {
	if limit <= 0 {
		limit = 50
	}
	err = m.logs(ctx).Where("session_id = ?", sessionID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

type SessionTokenAggregate struct {
	Calls int64 `gorm:"column:calls"`

	UsageReportedCalls int64 `gorm:"column:usage_reported_calls"`

	InputTokens  int64 `gorm:"column:input_tokens"`
	OutputTokens int64 `gorm:"column:output_tokens"`

	CachedCalls int64 `gorm:"column:cached_calls"`

	CachedInputTokens int64 `gorm:"column:cached_input_tokens"`

	CachedTokens int64 `gorm:"column:cached_tokens"`
}

func (m *CallLogModel) AggregateTokensBySession(ctx context.Context, sessionIDs []int64) (map[int64]SessionTokenAggregate, error) {
	out := make(map[int64]SessionTokenAggregate, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SessionID int64 `gorm:"column:session_id"`
		SessionTokenAggregate
	}
	err := m.db.WithContext(ctx).Model(&AICallLogEntity{}).
		Select(`session_id,
		        COUNT(*) AS calls,
		        COUNT(*) FILTER (WHERE usage_reported) AS usage_reported_calls,
		        COALESCE(SUM(input_tokens) FILTER (WHERE usage_reported), 0) AS input_tokens,
		        COALESCE(SUM(output_tokens) FILTER (WHERE usage_reported), 0) AS output_tokens,
		        COUNT(*) FILTER (WHERE cached_reported) AS cached_calls,
		        COALESCE(SUM(input_tokens) FILTER (WHERE cached_reported), 0) AS cached_input_tokens,
		        COALESCE(SUM(cached_tokens) FILTER (WHERE cached_reported), 0) AS cached_tokens`).
		Where("session_id IN ?", sessionIDs).
		Group("session_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.SessionID] = r.SessionTokenAggregate
	}
	return out, nil
}
