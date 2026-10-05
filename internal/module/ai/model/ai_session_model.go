// ai_session_model.go — AI 会话与事件日志的表访问单元（迁移 512）。
//
// 两张表是一件事的两半：ai_session 是会话头（序号分配、当前模型、压缩计数），
// ai_event 是它的事件日志（append-only 真源）。当前上下文永远由投影算出来（投影规则在
// service 层），本文件只负责取数与**原子分配序号**。
//
// 与 ai_model.go 的 ai_provider 表无关：那是配置面（一家供应商一行），这是会话面，
// 两者是不同聚合，本文件不读 ai_provider（用 provider_key / model_id 字符串解耦）。
package aimodel

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"

	aienums "go_wp/internal/module/ai/enums"
)

const (
	tableNameAISession = "ai_session"
	tableNameAIEvent   = "ai_event"
)

// AISessionEntity 对应 ai_session 表（列型真相在迁移 512；model 不声明列型）。
type AISessionEntity struct {
	ID            int64     `gorm:"column:id;primaryKey"`
	SessionKey    string    `gorm:"column:session_key"`
	Title         string    `gorm:"column:title"`
	ProviderKey   string    `gorm:"column:provider_key"`
	ModelID       string    `gorm:"column:model_id"`
	Status        int16     `gorm:"column:status"`
	NextSeq       int64     `gorm:"column:next_seq"`
	HeadSeq       int64     `gorm:"column:head_seq"`
	CompactCount  int       `gorm:"column:compact_count"`
	ContextTokens int64     `gorm:"column:context_tokens"`
	Version       int64     `gorm:"column:version"`
	CreateBy      int64     `gorm:"column:create_by"`
	CreateTime    time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateBy      int64     `gorm:"column:update_by"`
	UpdateTime    time.Time `gorm:"column:update_time;autoUpdateTime"`
}

// TableName 表名。
func (AISessionEntity) TableName() string { return tableNameAISession }

// AIEventEntity 对应 ai_event 表（append-only：本文件不提供 UPDATE / DELETE 方法）。
type AIEventEntity struct {
	ID             int64  `gorm:"column:id;primaryKey"`
	SessionID      int64  `gorm:"column:session_id"`
	Seq            int64  `gorm:"column:seq"`
	Kind           string `gorm:"column:kind"`
	SurfaceOp      string `gorm:"column:surface_op"`
	ReplaceFromSeq int64  `gorm:"column:replace_from_seq"`
	ReplaceToSeq   int64  `gorm:"column:replace_to_seq"`
	Content        string `gorm:"column:content"`
	ContentTokens  int64  `gorm:"column:content_tokens"`
	// UserID 这条事件是哪个后台账号写入的（535 起落库）。
	//
	// 与会话头上的 create_by 是两回事：那个记「谁开的会话」，这里记「这一条是谁写的」——
	// 一条会话可以被多个账号续写，审计要能追到每一条的发起人。
	// 0 表示未记录（535 之前写入的历史事件）。
	UserID int64 `gorm:"column:user_id"`
	// ProviderKey / ModelID：这条事件是哪个供应商、哪个模型产生的（529 起落库）。
	//
	// 与会话头上的同名字段是两回事：会话可以中途换模型，会话头记的是「当前用的那家」，
	// 这里记的是「这一条当时用的那家」—— 用量要按供应商/模型拆开，只能靠这两列。
	// 空串表示未记录（529 之前写入的历史事件），统计时要单独成组，不能并进某个供应商。
	ProviderKey string    `gorm:"column:provider_key"`
	ModelID     string    `gorm:"column:model_id"`
	Meta        JSONMap   `gorm:"column:meta;type:jsonb"`
	CreateTime  time.Time `gorm:"column:create_time;autoCreateTime"`
}

// TableName 表名。
func (AIEventEntity) TableName() string { return tableNameAIEvent }

// SessionModel 负责 ai_session / ai_event 两张表。
//
// 两张表共用一个 Model 的原因：它们是**同一个聚合**（会话与它的事件流），
// 「追加事件 + 更新会话计数」必须原子完成；拆成两个 Model 会让这个组合失去落脚点。
type SessionModel struct {
	db *gorm.DB
}

// NewSessionModel 构造。
func NewSessionModel(db *gorm.DB) *SessionModel { return &SessionModel{db: db} }

func (m *SessionModel) sessions(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AISessionEntity{})
}

func (m *SessionModel) events(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIEventEntity{})
}

// Transaction 在单个事务里执行 fn：追加事件与更新会话计数必须同一事务
// （否则会出现「事件写进去了但 head_seq 没动」，投影与计量当场分叉）。
func (m *SessionModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// —— 会话 ——

// CreateSessionTx 在事务里建会话。
//
// 首次追加事件时必须与事件写入同事务：否则建完会话、事件写失败会留下「0 事件空会话」脏数据。
func (m *SessionModel) CreateSessionTx(tx *gorm.DB, e *AISessionEntity) error {
	return tx.Model(&AISessionEntity{}).Create(e).Error
}

// FindSessionByKeyTx 事务内按会话键取；不存在返回 (nil, nil)。
func (m *SessionModel) FindSessionByKeyTx(tx *gorm.DB, key string) (*AISessionEntity, error) {
	var e AISessionEntity
	err := tx.Model(&AISessionEntity{}).Where("session_key = ?", key).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// FindSessionByIDTx 事务内按主键取；不存在返回 (nil, nil)。
func (m *SessionModel) FindSessionByIDTx(tx *gorm.DB, id int64) (*AISessionEntity, error) {
	var e AISessionEntity
	err := tx.Model(&AISessionEntity{}).Where("id = ?", id).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// CreateSession 建会话。session_key 撞唯一约束时原样返回错误，由 service 归因为「已存在」。
func (m *SessionModel) CreateSession(ctx context.Context, e *AISessionEntity) error {
	return m.sessions(ctx).Create(e).Error
}

// FindSessionByKey 按会话键取；不存在返回 (nil, nil)（「没有这个会话」由调用方决定怎么办）。
func (m *SessionModel) FindSessionByKey(ctx context.Context, key string) (*AISessionEntity, error) {
	var e AISessionEntity
	err := m.sessions(ctx).Where("session_key = ?", key).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// FindSessionByID 按主键取；不存在返回 (nil, nil)。
func (m *SessionModel) FindSessionByID(ctx context.Context, id int64) (*AISessionEntity, error) {
	var e AISessionEntity
	err := m.sessions(ctx).Where("id = ?", id).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListSessions 分页列出会话：keyword 为空不过滤（匹配标题或会话键），status < 0 不过滤状态。
//
// 排序固定「最近更新在前 + id 兜底」：分页时没有稳定次序会漏行 / 重行。
func (m *SessionModel) ListSessions(ctx context.Context, keyword string, status int, offset, limit int) (rows []AISessionEntity, total int64, err error) {
	q := m.sessions(ctx)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("title ILIKE ? OR session_key ILIKE ?", like, like)
	}
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = q.Order("update_time DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// NextSeqTx 在事务里原子分配下一个事件序号，返回分配到的 seq。
//
// 单语句 UPDATE … RETURNING：这一句本身就是行锁，并发追加在这里排队，拿到的序号必然互不相同
// （唯一约束只兜底、不参与正常路径），service 不需要写「重试到不撞车」的循环。
// 也因为它与事件插入同事务，失败回滚不会留下空号。会话不存在时返回 (0, false, nil)，与底层错误区分开。
func (m *SessionModel) NextSeqTx(tx *gorm.DB, sessionID int64) (seq int64, found bool, err error) {
	row := tx.Raw(
		"UPDATE ai_session SET next_seq = next_seq + 1 WHERE id = ? RETURNING next_seq - 1",
		sessionID,
	).Row()
	if scanErr := row.Scan(&seq); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, scanErr
	}
	return seq, true, nil
}

// UpdateSessionWithVersion 带乐观锁更新会话头（title / provider_key / model_id / status）。
//
// 守卫在 WHERE（id + version）：返回 0 行即「版本不符」，由 service 归因成冲突，
// 不做「先读出来算完再写回」（AGENTS.md：读-改-写必须有行锁或原子 SQL）。
func (m *SessionModel) UpdateSessionWithVersion(ctx context.Context, id, version int64, fields map[string]any, updateBy int64) (int64, error) {
	updates := map[string]any{
		"version":     gorm.Expr("version + 1"),
		"update_by":   updateBy,
		"update_time": gorm.Expr("now()"),
	}
	for k, v := range fields {
		updates[k] = v
	}
	res := m.sessions(ctx).Where("id = ? AND version = ?", id, version).Updates(updates)
	return res.RowsAffected, res.Error
}

// BumpAfterAppendTx 在事务里推进会话计量：head_seq 前进到刚分配的 seq，token 累加。
//
// 走 gorm.Expr 而不是 service 算好绝对值：并发追加时绝对值会互相覆盖。
func (m *SessionModel) BumpAfterAppendTx(tx *gorm.DB, sessionID, seq, addTokens, updateBy int64) error {
	res := tx.Model(&AISessionEntity{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"head_seq":       gorm.Expr("GREATEST(head_seq, ?)", seq),
			"context_tokens": gorm.Expr("context_tokens + ?", addTokens),
			"update_by":      updateBy,
			"update_time":    gorm.Expr("now()"),
		})
	return res.Error
}

// RecalcAfterCompactTx 在事务里重算会话计量：token 换成投影重算后的值，压缩次数 +1。
//
// 压缩后 token 是**重算值**而不是增量（折叠把若干条换成一条，增量算不出正确结果）。
func (m *SessionModel) RecalcAfterCompactTx(tx *gorm.DB, sessionID, tokens int64, updateBy int64) error {
	res := tx.Model(&AISessionEntity{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"context_tokens": tokens,
			"compact_count":  gorm.Expr("compact_count + 1"),
			"update_by":      updateBy,
			"update_time":    gorm.Expr("now()"),
		})
	return res.Error
}

// —— 事件 ——

// InsertEventTx 在事务里追加一条事件（seq 由 NextSeq 分配后传入）。
// InsertEventTx 事务内追加一条事件。
//
// Meta 归一：列是 NOT NULL DEFAULT '{}'，但 JSONMap(nil) 会被 gorm 序列化成 SQL NULL
// **显式写进 INSERT**（DB 默认值不会生效），于是「调用方没带 meta」会变成 23502。
// 在写库前补空对象，把「没有附加信息」表达成 '{}'。
func (m *SessionModel) InsertEventTx(tx *gorm.DB, e *AIEventEntity) error {
	if e.Meta == nil {
		e.Meta = JSONMap{}
	}
	return tx.Model(&AIEventEntity{}).Create(e).Error
}

// ListEventsFrom 取 fromSeq（含）之后的全部事件，按 seq 升序。
//
// limit <= 0 表示不限制。投影必须看到整段历史里所有改写指令，所以调用方通常从 1 取起；
// 展示侧想分页时用 ListEventsDesc。
func (m *SessionModel) ListEventsFrom(ctx context.Context, sessionID, fromSeq int64, limit int) (rows []AIEventEntity, err error) {
	q := m.events(ctx).Where("session_id = ? AND seq >= ?", sessionID, fromSeq).Order("seq ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&rows).Error
	return rows, err
}

// ListEventsFromTx 事务内取 fromSeq（含）之后的全部事件（折叠后重算计量必须看到本事务刚写的事件，
// 事务外的连接看不到未提交的行）。
func (m *SessionModel) ListEventsFromTx(tx *gorm.DB, sessionID, fromSeq int64) (rows []AIEventEntity, err error) {
	err = tx.Model(&AIEventEntity{}).
		Where("session_id = ? AND seq >= ?", sessionID, fromSeq).
		Order("seq ASC").Find(&rows).Error
	return rows, err
}

// ListEventsDesc 倒序分页取事件（后台「事件日志」列表用：最近的在最上面）。
func (m *SessionModel) ListEventsDesc(ctx context.Context, sessionID int64, offset, limit int) (rows []AIEventEntity, total int64, err error) {
	q := m.events(ctx).Where("session_id = ?", sessionID)
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = q.Order("seq DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// SumTokensFrom 汇总 fromSeq（含）之后事件的 token 数（投影重算用；折叠指令自身也算）。
func (m *SessionModel) SumTokensFrom(ctx context.Context, sessionID, fromSeq int64) (int64, error) {
	var row struct {
		Total int64 `gorm:"column:total"`
	}
	err := m.events(ctx).Select("COALESCE(SUM(content_tokens), 0) AS total").
		Where("session_id = ? AND seq >= ?", sessionID, fromSeq).Take(&row).Error
	if err != nil {
		return 0, err
	}
	return row.Total, nil
}

// —— 用量统计与多维筛选 ——
//
// 这一段的读法与上面的会话 CRUD 不同：它按**维度**汇总（一段时间 / 一家供应商 / 一个模型 /
// 一个创建人），供后台会话页的指标卡与趋势图使用。两张表仍是同一个聚合（会话 + 它的事件流），
// 所以 JOIN 只发生在本文件内部，不跨模块。

// SessionQuery 会话列表 / 统计的筛选条件。零值即不过滤，唯一例外是 Status：
// 用 -1 表达「不限状态」而不是 0（0 是「已归档」这一真实取值）。
type SessionQuery struct {
	Keyword     string
	Status      int
	ProviderKey string
	ModelID     string
	CreateBy    int64
	From        *time.Time
	To          *time.Time
}

// applySessionQuery 把筛选条件落成 WHERE。
//
// 每个维度都只在**有值**时才追加条件：把「不限」写成 `provider_key = ”` 会把结果收窄成
// 「没记供应商的那几条」，与「不过滤」正好相反。时间范围按会话创建时间取闭区间
// （To 当天 23:59:59 也算在内，实现上是 < To+1 天 —— 用 <= To 会把当天 00:00 之后的都漏掉）。
func applySessionQuery(q *gorm.DB, f SessionQuery) *gorm.DB {
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("title ILIKE ? OR session_key ILIKE ?", like, like)
	}
	if f.Status >= 0 {
		q = q.Where("status = ?", f.Status)
	}
	if f.ProviderKey != "" {
		q = q.Where("provider_key = ?", f.ProviderKey)
	}
	if f.ModelID != "" {
		q = q.Where("model_id = ?", f.ModelID)
	}
	if f.CreateBy > 0 {
		q = q.Where("create_by = ?", f.CreateBy)
	}
	if f.From != nil {
		q = q.Where("create_time >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("create_time < ?", f.To.AddDate(0, 0, 1))
	}
	return q
}

// ListSessionsFiltered 多条件分页列会话。排序与 ListSessions 保持一致（最近更新在前 + id 兜底），
// 否则改一次筛选条件就会让同一行在不同页之间跳动。
func (m *SessionModel) ListSessionsFiltered(ctx context.Context, f SessionQuery, offset, limit int) (rows []AISessionEntity, total int64, err error) {
	q := applySessionQuery(m.sessions(ctx), f)
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = q.Order("update_time DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// SessionUsage 一批会话的用量汇总（指标卡）。
type SessionUsage struct {
	Sessions int64 `gorm:"column:sessions"`
	Events   int64 `gorm:"column:events"`
	Tokens   int64 `gorm:"column:tokens"`
	Compacts int64 `gorm:"column:compacts"`

	// —— 以下来自**调用流水**（ai_call_log）与摘要事件，口径与上面的 Tokens 不同 ——
	Calls              int64 `gorm:"column:calls"`
	UsageReportedCalls int64 `gorm:"column:usage_reported_calls"`
	CachedCalls        int64 `gorm:"column:cached_calls"`
	CachedInputTokens  int64 `gorm:"column:cached_input_tokens"`
	CachedTokens       int64 `gorm:"column:cached_tokens"`
	SummaryTokens      int64 `gorm:"column:summary_tokens"`
}

// SessionUsageOf 汇总匹配会话的：会话数、事件数、累计 token（事件正文估算值之和）、压缩次数。
//
// 分两条语句而不是一条 JOIN：会话侧是「一行一票」的计数，事件侧是「一行一条事件」的计数，
// 并成一个 GROUP BY 会让 SUM(compact_count) 被事件行数放大（一条会话 30 条事件就多算 30 倍）。
// 两次读之间没有事务 —— 这里是只读看板，某一秒的新增事件造成两个数字轻微不同步是可接受的，
// 反过来为了对齐而把整个页面包进事务会让长查询长时间持锁。
func (m *SessionModel) SessionUsageOf(ctx context.Context, f SessionQuery) (out SessionUsage, err error) {
	var head SessionUsage
	if err = applySessionQuery(m.sessions(ctx), f).
		Select("COUNT(*) AS sessions, COALESCE(SUM(compact_count), 0) AS compacts").
		Take(&head).Error; err != nil {
		return out, err
	}
	var body struct {
		Events int64 `gorm:"column:events"`
		Tokens int64 `gorm:"column:tokens"`
	}
	if err = m.events(ctx).
		Where("session_id IN (?)", applySessionQuery(m.sessions(ctx), f).Select("id")).
		Select("COUNT(*) AS events, COALESCE(SUM(content_tokens), 0) AS tokens").
		Take(&body).Error; err != nil {
		return out, err
	}
	out = SessionUsage{
		Sessions: head.Sessions,
		Compacts: head.Compacts,
		Events:   body.Events,
		Tokens:   body.Tokens,
	}
	// 调用流水侧：命中率要用上游上报的 input / cached（事件表上没有这两个数）。
	//
	// 单独一条查询而不是并进上面：它们的**筛选口径相同但数据源不同** ——
	// 把两张表 join 起来算会让「没有调用流水的会话」从结果里消失，
	// 而那种会话恰恰要显示成「无数据」而不是「0 次调用」。
	var calls struct {
		Calls              int64 `gorm:"column:calls"`
		UsageReportedCalls int64 `gorm:"column:usage_reported_calls"`
		CachedCalls        int64 `gorm:"column:cached_calls"`
		CachedInputTokens  int64 `gorm:"column:cached_input_tokens"`
		CachedTokens       int64 `gorm:"column:cached_tokens"`
	}
	if err = m.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) AS calls,
		        COUNT(*) FILTER (WHERE usage_reported) AS usage_reported_calls,
		        COUNT(*) FILTER (WHERE cached_reported) AS cached_calls,
		        COALESCE(SUM(input_tokens) FILTER (WHERE cached_reported), 0) AS cached_input_tokens,
		        COALESCE(SUM(cached_tokens) FILTER (WHERE cached_reported), 0) AS cached_tokens
		   FROM ai_call_log
		  WHERE session_id IN (?)`,
		applySessionQuery(m.sessions(ctx), f).Select("id"),
	).Scan(&calls).Error; err != nil {
		return out, err
	}
	out.Calls = calls.Calls
	out.UsageReportedCalls = calls.UsageReportedCalls
	out.CachedCalls = calls.CachedCalls
	out.CachedInputTokens = calls.CachedInputTokens
	out.CachedTokens = calls.CachedTokens

	// 摘要占的上下文：compact_summary 事件的内容 token 之和。
	//
	// 用 kind 而不是 surface_op 判：surface_op=replace 的事件只可能是摘要，
	// 但 kind 是更稳的判据（将来若有别的 replace 形态，用 surface_op 会让它们一起被算进来）。
	var summary struct {
		SummaryTokens int64 `gorm:"column:summary_tokens"`
	}
	if err = m.events(ctx).
		Where("session_id IN (?)", applySessionQuery(m.sessions(ctx), f).Select("id")).
		Where("kind = ?", string(aienums.EventKindCompactSummary)).
		Select("COALESCE(SUM(content_tokens), 0) AS summary_tokens").
		Take(&summary).Error; err != nil {
		return out, err
	}
	out.SummaryTokens = summary.SummaryTokens
	return out, nil
}

// TrendSeriesRow 折线图上的一个分组点：(供应商, 模型, 天) → token。
type TrendSeriesRow struct {
	ProviderKey string
	ModelID     string
	Day         time.Time
	Tokens      int64
}

// SessionTokenTrendBySeries 按 (供应商, 模型) 分组的逐日 token —— 折线图上的多条线。
//
// 折线图必须按来源拆开才看得出「是哪家在消耗」：旧的合计口径（SessionTokenTrend）在
// 多序列的图上没有位置，已随柱状图一起删除。筛选（同一个 session 子查询）与 trunc 白名单沿用同一套。
func (m *SessionModel) SessionTokenTrendBySeries(ctx context.Context, f SessionQuery, since time.Time, trunc string) (rows []TrendSeriesRow, err error) {
	if trunc != "week" {
		trunc = "day"
	}
	err = m.events(ctx).
		Where("session_id IN (?)", applySessionQuery(m.sessions(ctx), f).Select("id")).
		Where("create_time >= ?", since).
		Select("provider_key, model_id, date_trunc('" + trunc + "', create_time)::date AS day, COALESCE(SUM(content_tokens), 0) AS tokens").
		Group("provider_key, model_id, day").Order("day ASC").Find(&rows).Error
	return rows, err
}

// SessionFacets 会话上出现过的筛选取值（供下拉候选）。//
// 取全量而不是「当前结果集的取值」：筛成 A 供应商后再想切到 B，下拉里必须还有 B
// —— 否则筛一次就再也回不去（这是筛选器最常见的自锁）。
type SessionFacets struct {
	Providers []string
	Models    []string
	Creators  []int64
}

// SessionFacetsOf 取三组候选值，各自去重升序。
func (m *SessionModel) SessionFacetsOf(ctx context.Context) (out SessionFacets, err error) {
	if err = m.sessions(ctx).Where("provider_key <> ''").
		Distinct().Order("provider_key ASC").
		Pluck("provider_key", &out.Providers).Error; err != nil {
		return out, err
	}
	if err = m.sessions(ctx).Where("model_id <> ''").
		Distinct().Order("model_id ASC").
		Pluck("model_id", &out.Models).Error; err != nil {
		return out, err
	}
	err = m.sessions(ctx).Where("create_by > 0").
		Distinct().Order("create_by ASC").
		Pluck("create_by", &out.Creators).Error
	return out, err
}

// SessionKindUsage 一个会话按事件类型汇总的消耗（抽屉里的 token 明细）。
type SessionKindUsage struct {
	Kind   string
	Events int64
	Tokens int64
}

// SessionKindUsageOf 按 kind 分组统计事件数与正文 token。
//
// 只查这一个会话：kind 的取值由写入侧白名单约束（user / assistant / tool / compact_* / note），
// 这里原样返回，译成中文标签是 service 的事 —— model 不认识展示层。
// 排序按该类型首次出现的序号，让「用户 → 助手 → 工具 → 折叠」这个自然顺序稳定下来
// （按 kind 字符串排会得到 assistant/tool/user，读起来是乱的）。
func (m *SessionModel) SessionKindUsageOf(ctx context.Context, sessionID int64) (rows []SessionKindUsage, err error) {
	err = m.events(ctx).
		Select("kind, COUNT(*) AS events, COALESCE(SUM(content_tokens), 0) AS tokens").
		Where("session_id = ?", sessionID).
		Group("kind").
		Order("MIN(seq) ASC").
		Scan(&rows).Error
	return rows, err
}

// SessionModelUsage 一个会话里按 (供应商, 模型) 拆开的消耗。
type SessionModelUsage struct {
	SessionID   int64
	ProviderKey string
	ModelID     string
	Events      int64
	Tokens      int64
}

// SessionModelUsageOf 批量取一组会话的按 (供应商, 模型) 拆分明细。
//
// 一次查完整个列表页：列表 20 行逐行查就是 20 次往返，而这块数据的用法是「鼠标一停就要看到」，
// 不能让悬浮卡在等第 20 次查询。空 provider_key / model_id 归为「未记录」一组 ——
// 529 之前写入的历史事件就是这一类，不能把它们并进某个供应商的总量里。
//
// 排序按消耗降序：最贵的那家排在最上面，这才是打开明细要找的东西。
func (m *SessionModel) SessionModelUsageOf(ctx context.Context, sessionIDs []int64) (rows []SessionModelUsage, err error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	err = m.events(ctx).
		Select("session_id, provider_key, model_id, COUNT(*) AS events, COALESCE(SUM(content_tokens), 0) AS tokens").
		Where("session_id IN ?", sessionIDs).
		Group("session_id, provider_key, model_id").
		Order("session_id ASC, SUM(content_tokens) DESC").
		Scan(&rows).Error
	return rows, err
}
