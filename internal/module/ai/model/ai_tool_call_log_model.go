// ai_tool_call_log_model.go — 模型工具调用流水（迁移 541）。
//
// 与 ai_call_log（537）的分工：
//
//	ai_call_log      —— 「打了一次上游大模型」。列是出站调用的形状（provider / model / token）。
//	ai_tool_call_log —— 「模型调了一次我们的工具」。列是工具的形状（tool_name / status / truncated）。
//
// 两者相同的是写入纪律：旁路观测、协程异步写、写失败不影响业务结果、只增不改。
//
// 只存摘要：参数与结果的原文在 ai_event 的工具事件里（append-only 真源、参与投影）；
// 本表只回答「发生过、结论是什么、多大、被拒了几次」，读面更宽（将来给外部 /mcp 用），
// 所以不放可能含业务敏感数据的全文。
//
// 只增不改：本文件不提供 UPDATE / DELETE（流水被改写就不再是流水）。
package aimodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameAIToolCallLog = "ai_tool_call_log"

// AIToolCallLogEntity 对应 ai_tool_call_log 表（列型真相在迁移 541；model 不声明列型）。
type AIToolCallLogEntity struct {
	ID int64 `gorm:"column:id;primaryKey"`
	// SessionID / UserID：这次调用属于哪条会话、由哪个后台账号发起。
	// 0 表示未记录（服务端从登录态取 UserID，不采信请求参数）。
	SessionID int64 `gorm:"column:session_id"`
	UserID    int64 `gorm:"column:user_id"`
	// ToolName 是注册表里的工具名（不是模型给的原始字符串：未知名也会记在这里，便于发现乱调）。
	ToolName string `gorm:"column:tool_name"`
	// ArgumentsSummary / ResultSummary 是**摘要**（超长截断）；原文在 ai_event 的工具事件里。
	ArgumentsSummary string `gorm:"column:arguments_summary"`
	ResultSummary    string `gorm:"column:result_summary"`
	// ResultLen 是结果原文长度；summary 被截断时这里仍是全量长度（用来判断「是不是真的大」）。
	ResultLen int64 `gorm:"column:result_len"`
	// Truncated 标记交给模型的结果是否被剪枝过 —— 排查「模型为什么没用上完整数据」看这一列。
	Truncated bool `gorm:"column:truncated"`
	// Status 取值见 aienums.ToolCallStatus；ErrorKey 是失败时的 i18n key（与 enums 哨兵同源）。
	// 表里**不放底层原文**：上游与工具的错误可能回显内部标识，原文只进日志。
	Status string `gorm:"column:status"`
	// ErrorKey 失败时的 i18n key；成功时为空串。
	ErrorKey string `gorm:"column:error_key"`
	// LatencyMs 工具执行耗时（含权限判定与参数校验）。
	LatencyMs  int64     `gorm:"column:latency_ms"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
}

// TableName 表名。
func (AIToolCallLogEntity) TableName() string { return tableNameAIToolCallLog }

// ToolCallLogModel 负责 ai_tool_call_log 一张表。
type ToolCallLogModel struct {
	db *gorm.DB
}

// NewToolCallLogModel 构造。
func NewToolCallLogModel(db *gorm.DB) *ToolCallLogModel { return &ToolCallLogModel{db: db} }

func (m *ToolCallLogModel) logs(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIToolCallLogEntity{})
}

// Insert 追加一条工具调用流水。错误由调用方决定怎么办（出站层只记日志、不打断业务）。
func (m *ToolCallLogModel) Insert(ctx context.Context, e *AIToolCallLogEntity) error {
	if e == nil {
		return nil
	}
	return m.logs(ctx).Create(e).Error
}

// ListBySession 取某条会话的工具调用流水（倒序，最近的在最前）。
//
// 排序用 id 而不是 create_time：异步写意味着落库顺序与调用顺序可能在并发下不一致，
// 而 id 是自增的落库顺序 —— 排查时要看的是「库里按什么次序进来的」。limit <= 0 回落 50。
func (m *ToolCallLogModel) ListBySession(ctx context.Context, sessionID int64, limit int) (rows []AIToolCallLogEntity, err error) {
	if limit <= 0 {
		limit = 50
	}
	err = m.logs(ctx).Where("session_id = ?", sessionID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
