// ai_call_log_model.go — 大模型调用流水（迁移 537）。
//
// 与 ai_event 的分工（两者刻意分开，不是一个聚合）：
//
//	ai_event   —— 「会话里说了什么」。业务真源，append-only，参与投影与计量；写不进去会话就残了。
//	ai_call_log —— 「打了一次上游」。旁路观测：谁调的、用哪家模型、多久、成没成、报了多少 token；
//	               不参与投影、不参与计量，写失败不影响任何业务结果。
//
// 分开的理由：把观测数据塞进事件流会让「日志写失败」变成「对话失败」，
// 也会让投影多出一堆不该进上下文的行。
//
// 只增不改：本文件不提供 UPDATE / DELETE（流水被改写就不再是流水）。
package aimodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameAICallLog = "ai_call_log"

// AICallLogEntity 对应 ai_call_log 表（列型真相在迁移 537；model 不声明列型）。
type AICallLogEntity struct {
	ID int64 `gorm:"column:id;primaryKey"`
	// SessionID / UserID：这次调用属于哪条会话、由哪个后台账号发起。
	// 0 表示未记录（对外接口没有会话概念；UserID 由服务端从登录态取，不采信请求参数）。
	SessionID int64 `gorm:"column:session_id"`
	UserID    int64 `gorm:"column:user_id"`
	// ProviderKey / ModelID / Protocol：**实际出站用的**那三个值（不是请求里写的原文）。
	ProviderKey string `gorm:"column:provider_key"`
	ModelID     string `gorm:"column:model_id"`
	Protocol    string `gorm:"column:protocol"`
	// 上游上报的用量。UsageReported=false 时三列恒为 0，语义是「这家没上报」而不是「用了 0 个」。
	InputTokens  int64 `gorm:"column:input_tokens"`
	OutputTokens int64 `gorm:"column:output_tokens"`
	TotalTokens  int64 `gorm:"column:total_tokens"`
	// CachedTokens 输入里命中上游前缀缓存的 token 数（迁移 566）。
	//
	// 它是 docs/16 §3.1 第一个验收数字（命中率 95–97%）的唯一数据源 —— 本地算不出来，
	// 只能采信上游。CachedReported 与它分开的原因见 service 层 ReplyUsage 的注释：
	// 「没报」与「报了 0」必须能分开，否则命中率会把没报的调用算成未命中。
	CachedTokens   int64 `gorm:"column:cached_tokens"`
	CachedReported bool  `gorm:"column:cached_reported"`
	UsageReported  bool  `gorm:"column:usage_reported"`
	// LatencyMs 从发起到拿到响应的墙钟耗时（含 SSRF 校验与解析），排查慢调用用。
	LatencyMs int64 `gorm:"column:latency_ms"`
	// Status 取值见 aienums.CallStatus；ErrorKey 是失败时的 i18n key（与 enums 哨兵同源）。
	// 表里**不放底层原文**：上游报文可能回显密钥或内部标识，原文只进日志。
	Status     string    `gorm:"column:status"`
	ErrorKey   string    `gorm:"column:error_key"`
	CreateTime time.Time `gorm:"column:create_time;autoCreateTime"`
}

// TableName 表名。
func (AICallLogEntity) TableName() string { return tableNameAICallLog }

// CallLogModel 负责 ai_call_log 一张表。
//
// 单独一个 Model 而不是并进 SessionModel：调用流水与会话不是同一个聚合 ——
// 它不参与会话的任何计量与投影，只被出站层写、被排查时读。
type CallLogModel struct {
	db *gorm.DB
}

// NewCallLogModel 构造。
func NewCallLogModel(db *gorm.DB) *CallLogModel { return &CallLogModel{db: db} }

func (m *CallLogModel) logs(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AICallLogEntity{})
}

// Insert 追加一条调用流水。错误由调用方决定怎么办（出站层只记日志、不打断业务）。
func (m *CallLogModel) Insert(ctx context.Context, e *AICallLogEntity) error {
	if e == nil {
		return nil
	}
	return m.logs(ctx).Create(e).Error
}

// defaultSessionCallLimit 未指定条数时每个会话取多少条。
const defaultSessionCallLimit = 5

// SessionCallLogRow 一条调用流水 + 它在所属会话内的排名与总数（窗口函数一次算完）。
type SessionCallLogRow struct {
	AICallLogEntity
	// Total 该会话的调用总条数（不是这一行的，是整个会话的）。
	Total int64 `gorm:"column:total"`
	// Rank 本行在会话内的倒序排名，1 = 最近一次。
	Rank int64 `gorm:"column:rank"`
}

// RecentCallsBySession 批量取一组会话各自的最近 perSession 条调用流水。
//
// 一次查完整页：列表 20 行逐行查就是 20 次往返，而这块数据是「鼠标一停就要看到」的。
// 用窗口函数而不是「每个会话一条 LIMIT 子查询」：后者要么写成 N 条 UNION，要么上 LATERAL，
// 可读性都更差；窗口函数一次扫完，还能顺手带出每个会话的总条数
// （悬浮卡得告诉读的人「你看到的是不是全部」）。
//
// 排序 (session_id, rn)：调用方按会话分组消费，同组内最近的在最前。
func (m *CallLogModel) RecentCallsBySession(ctx context.Context, sessionIDs []int64, perSession int) (rows []SessionCallLogRow, err error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	if perSession <= 0 {
		perSession = defaultSessionCallLimit
	}
	// 列别名用 rn 而不是 rank：rank 是 SQL 的窗口函数名，写成同名别名时
	// ORDER BY 那一处会与函数名撞脸（PG 能解析，但读的人要先想一下）。
	// SQL 写成**一个**原始字符串字面量（不跨行拼 +）：拼接式续行时 Go 不会补空格，
	// 上一段的末尾与下一段的开头会粘在一起（实测把 `AS rn` 与 `FROM` 拼成 `rnFROM`），
	// 而这类错误只在真跑一次查询时才会暴露。
	inner := m.db.WithContext(ctx).
		Table(tableNameAICallLog+" AS c").
		Select("c.*, COUNT(*) OVER (PARTITION BY c.session_id) AS total, "+
			"ROW_NUMBER() OVER (PARTITION BY c.session_id ORDER BY c.id DESC) AS rn").
		Where("c.session_id IN ?", sessionIDs)
	// FROM 子查询：`Table("(?) AS t", inner)` 是 GORM 里表达「套一层再过滤」的正规出口。
	err = m.db.WithContext(ctx).
		Table("(?) AS t", inner).
		Where("t.rn <= ?", perSession).
		Order("t.session_id ASC, t.rn ASC").
		Scan(&rows).Error
	return rows, err
}

// ListBySession 取某条会话的调用流水（倒序，最近的在最前）。
//
// 排序用 id 而不是 create_time：异步写意味着落库顺序与调用顺序**可能**在并发下不一致，
// 而 id 是自增的落库顺序 —— 排查时要看的是「库里按什么次序进来的」。limit <= 0 回落 50。
func (m *CallLogModel) ListBySession(ctx context.Context, sessionID int64, limit int) (rows []AICallLogEntity, err error) {
	if limit <= 0 {
		limit = 50
	}
	err = m.logs(ctx).Where("session_id = ?", sessionID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// SessionTokenAggregate 一条会话的 token 侧聚合（docs/16 §3.1 的命中率要用它）。
//
// 分成「报了缓存字段的调用」与「全部调用」两组计数，而不是一个总数：
// 命中率的分母只能是**报了**的那些（见 ReplyUsage.CachedReported 的注释）——
// 把没报的算进分母会把命中率拉低一大截，而失真的方向恰好是「看起来更差」，
// 看到难看数字的人会去调提示词，不会想到是上游没报。
type SessionTokenAggregate struct {
	// Calls 全部调用条数（含没报用量的）。
	Calls int64 `gorm:"column:calls"`
	// UsageReportedCalls 上报了用量的调用条数（命中率与开销的可用性判据）。
	UsageReportedCalls int64 `gorm:"column:usage_reported_calls"`
	// InputTokens / OutputTokens 全部上报的用量之和。
	InputTokens  int64 `gorm:"column:input_tokens"`
	OutputTokens int64 `gorm:"column:output_tokens"`
	// CachedCalls 报了缓存字段的调用条数。
	CachedCalls int64 `gorm:"column:cached_calls"`
	// CachedInputTokens 那些调用的输入之和（命中率的分母）。
	CachedInputTokens int64 `gorm:"column:cached_input_tokens"`
	// CachedTokens 那些调用命中的 token 之和（命中率的分子）。
	CachedTokens int64 `gorm:"column:cached_tokens"`
}

// AggregateTokensBySession 批量取一组会话的 token 聚合。
//
// 一次查完整批：会话列表每行都要显示命中率，逐行查就是 20 次往返。
// 空入参直接回空 map（不查库、也不返回 nil 让调用方判空指针）。
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
