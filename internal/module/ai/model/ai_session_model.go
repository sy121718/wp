package aimodel

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	aienums "go_wp/internal/module/ai/enums"
	json "go_wp/pkg/jsonx"
)

const (
	tableNameAISession = "ai_session"
	tableNameAIEvent   = "ai_event"
)

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

func (AISessionEntity) TableName() string { return tableNameAISession }

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

	UserID int64 `gorm:"column:user_id"`

	ProviderKey string    `gorm:"column:provider_key"`
	ModelID     string    `gorm:"column:model_id"`
	Meta        JSONMap   `gorm:"column:meta;type:jsonb"`
	CreateTime  time.Time `gorm:"column:create_time;autoCreateTime"`
}

func (AIEventEntity) TableName() string { return tableNameAIEvent }

type SessionModel struct {
	db *gorm.DB
}

func NewSessionModel(db *gorm.DB) *SessionModel { return &SessionModel{db: db} }

func (m *SessionModel) sessions(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AISessionEntity{})
}

func (m *SessionModel) events(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIEventEntity{})
}

func (m *SessionModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// TransactionWithOptions 是带隔离级别的只读事务。
//
// 用在「一块屏上的几个数字必须出自同一份快照」的地方：用量卡片的会话数、事件数、
// token 数、压缩成本来自四条不同语句，READ COMMITTED 下每条各看各的时刻 ——
// 分子分母可能来自不同瞬间，比出来的百分比是假的。REPEATABLE READ 让四条读到同一份快照。
func (m *SessionModel) TransactionWithOptions(ctx context.Context, opts *sql.TxOptions, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn, opts)
}

func (m *SessionModel) CreateSessionTx(tx *gorm.DB, e *AISessionEntity) error {
	return tx.Model(&AISessionEntity{}).Create(e).Error
}

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

// LockSessionByKeyTx 按 session_key 取会话并加行锁。
//
// 追加事件走 key 分支时必须用它而不是 FindSessionByKeyTx：写入要基于 NextSeq 递增，
// 不加锁的话两个并发追加会读到同一个 seq，靠唯一约束把其中一个打回去重试。
// 锁在 session 行上，与按 id 的分支锁的是同一类对象（同一张表、同一把主键行）。
func (m *SessionModel) LockSessionByKeyTx(tx *gorm.DB, key string) (*AISessionEntity, error) {
	var e AISessionEntity
	err := tx.Model(&AISessionEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_key = ?", key).
		Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// LockSessionByIDTx 按主键取会话并加行锁（追加事件走 id 分支时用）。
func (m *SessionModel) LockSessionByIDTx(tx *gorm.DB, id int64) (*AISessionEntity, error) {
	var e AISessionEntity
	err := tx.Model(&AISessionEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

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

func (m *SessionModel) CreateSession(ctx context.Context, e *AISessionEntity) error {
	return m.sessions(ctx).Create(e).Error
}

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

func (m *SessionModel) NextSeqTx(tx *gorm.DB, sessionID int64) (seq int64, found bool, err error) {
	var sess AISessionEntity

	res := tx.Model(&sess).
		Where("id = ?", sessionID).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "next_seq"}}}).
		UpdateColumn("next_seq", gorm.Expr("next_seq + 1"))
	if res.Error != nil {
		return 0, false, res.Error
	}
	if res.RowsAffected == 0 {

		return 0, false, nil
	}

	return sess.NextSeq - 1, true, nil
}

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

func (m *SessionModel) InsertEventTx(tx *gorm.DB, e *AIEventEntity) error {
	if e.Meta == nil {
		e.Meta = JSONMap{}
	}
	return tx.Model(&AIEventEntity{}).Create(e).Error
}

// MarkEventMeta 把若干键并进某条事件的 meta（jsonb 合并，不整体覆盖）。
//
// 为什么是「并入」而不是「改写」：一条事件可能同时带 userText / imageLabels /
// reasoning 等多个键，补记失败状态的调用方只知道自己的那个键，整体覆盖会把别人的
// 事实抹掉。用 `||` 让数据库做键级合并，读-改-写就没有窗口。
func (m *SessionModel) MarkEventMeta(ctx context.Context, sessionID, seq int64, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return m.events(ctx).
		Where("session_id = ? AND seq = ?", sessionID, seq).
		UpdateColumn("meta", gorm.Expr("meta || ?::jsonb", string(raw))).Error
}

func (m *SessionModel) ListEventsFrom(ctx context.Context, sessionID, fromSeq int64, limit int) (rows []AIEventEntity, err error) {
	q := m.events(ctx).Where("session_id = ? AND seq >= ?", sessionID, fromSeq).Order("seq ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&rows).Error
	return rows, err
}

func (m *SessionModel) ListEventsFromTx(tx *gorm.DB, sessionID, fromSeq int64) (rows []AIEventEntity, err error) {
	err = tx.Model(&AIEventEntity{}).
		Where("session_id = ? AND seq >= ?", sessionID, fromSeq).
		Order("seq ASC").Find(&rows).Error
	return rows, err
}

// ListFailedRounds 找出「用户说了话、但这一轮没有拿到回复」的事件序号。
//
// 这是 replyState 标记的读侧：sendMessage 在外部调用失败时把它补记在用户事件的 meta
// 上，重放/运维靠它定位要重试哪几轮，不必扫全表比对 user/assistant 配对。
// 返回 session_id / seq / meta 三个字段：调用方要从 meta 里读 replyError（失败原因），
// 少选这一列会让原因永远是空串 —— 那等于把「为什么会失败」这条事实丢掉。
// 仍不带 content：读侧只定位要重试哪几轮，不需要正文。
func (m *SessionModel) ListFailedRounds(ctx context.Context, sessionID int64, limit int) (rows []AIEventEntity, err error) {
	q := m.events(ctx).
		Select("session_id, seq, meta").
		Where("session_id = ? AND meta ->> 'replyState' = ?", sessionID, "failed").
		Order("seq ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&rows).Error
	return rows, err
}

func (m *SessionModel) ListEventsDesc(ctx context.Context, sessionID int64, offset, limit int) (rows []AIEventEntity, total int64, err error) {
	q := m.events(ctx).Where("session_id = ?", sessionID)
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = q.Order("seq DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

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

type SessionQuery struct {
	Keyword     string
	Status      int
	ProviderKey string
	ModelID     string
	CreateBy    int64
	From        *time.Time
	To          *time.Time
}

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

func (m *SessionModel) ListSessionsFiltered(ctx context.Context, f SessionQuery, offset, limit int) (rows []AISessionEntity, total int64, err error) {
	q := applySessionQuery(m.sessions(ctx), f)
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = q.Order("update_time DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

type SessionUsage struct {
	Sessions int64 `gorm:"column:sessions"`
	Events   int64 `gorm:"column:events"`
	Tokens   int64 `gorm:"column:tokens"`
	Compacts int64 `gorm:"column:compacts"`

	Calls              int64 `gorm:"column:calls"`
	UsageReportedCalls int64 `gorm:"column:usage_reported_calls"`
	CachedCalls        int64 `gorm:"column:cached_calls"`
	CachedInputTokens  int64 `gorm:"column:cached_input_tokens"`
	CachedTokens       int64 `gorm:"column:cached_tokens"`
	SummaryTokens      int64 `gorm:"column:summary_tokens"`
}

func (m *SessionModel) SessionUsageOf(ctx context.Context, f SessionQuery) (out SessionUsage, err error) {
	// 一段一块屏上的数字必须同源：会话数 / 事件数 / token / 压缩成本 / 调用与缓存命中
	// 分属四条语句，READ COMMITTED 下各看各的时刻，「压缩成本占比」的分子分母可能
	// 来自不同瞬间。开一个 REPEATABLE READ 只读事务，让四条读到同一份快照。
	err = m.TransactionWithOptions(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true},
		func(tx *gorm.DB) error {
			return m.sessionUsageTx(tx, f, &out)
		})
	if err != nil {
		return SessionUsage{}, err
	}
	return out, nil
}

// sessionUsageTx 在给定事务里跑完四段聚合，调用方保证它们同快照。
func (m *SessionModel) sessionUsageTx(tx *gorm.DB, f SessionQuery, out *SessionUsage) error {
	sessionsTx := tx.Model(&AISessionEntity{})

	var head SessionUsage
	if err := applySessionQuery(sessionsTx, f).
		Select("COUNT(*) AS sessions, COALESCE(SUM(compact_count), 0) AS compacts").
		Take(&head).Error; err != nil {
		return err
	}
	// 子查询也用同一个 tx 句柄：换成 m.sessions(ctx) 会另开一条连接，看到的是另一份快照。
	inSessions := applySessionQuery(tx.Model(&AISessionEntity{}), f).Select("id")

	var body struct {
		Events int64 `gorm:"column:events"`
		Tokens int64 `gorm:"column:tokens"`
	}
	if err := tx.Model(&AIEventEntity{}).
		Where("session_id IN (?)", inSessions).
		Select("COUNT(*) AS events, COALESCE(SUM(content_tokens), 0) AS tokens").
		Take(&body).Error; err != nil {
		return err
	}
	out.Sessions = head.Sessions
	out.Compacts = head.Compacts
	out.Events = body.Events
	out.Tokens = body.Tokens

	var calls struct {
		Calls              int64 `gorm:"column:calls"`
		UsageReportedCalls int64 `gorm:"column:usage_reported_calls"`
		CachedCalls        int64 `gorm:"column:cached_calls"`
		CachedInputTokens  int64 `gorm:"column:cached_input_tokens"`
		CachedTokens       int64 `gorm:"column:cached_tokens"`
	}
	if err := tx.Model(&AICallLogEntity{}).
		Select(`COUNT(*) AS calls,
		        COUNT(*) FILTER (WHERE usage_reported) AS usage_reported_calls,
		        COUNT(*) FILTER (WHERE cached_reported) AS cached_calls,
		        COALESCE(SUM(input_tokens) FILTER (WHERE cached_reported), 0) AS cached_input_tokens,
		        COALESCE(SUM(cached_tokens) FILTER (WHERE cached_reported), 0) AS cached_tokens`).
		Where("session_id IN (?)", applySessionQuery(tx.Model(&AISessionEntity{}), f).Select("id")).
		Scan(&calls).Error; err != nil {
		return err
	}
	out.Calls = calls.Calls
	out.UsageReportedCalls = calls.UsageReportedCalls
	out.CachedCalls = calls.CachedCalls
	out.CachedInputTokens = calls.CachedInputTokens
	out.CachedTokens = calls.CachedTokens

	var summary struct {
		SummaryTokens int64 `gorm:"column:summary_tokens"`
	}
	if err := tx.Model(&AIEventEntity{}).
		Where("session_id IN (?)", applySessionQuery(tx.Model(&AISessionEntity{}), f).Select("id")).
		Where("kind = ?", string(aienums.EventKindCompactSummary)).
		Select("COALESCE(SUM(content_tokens), 0) AS summary_tokens").
		Take(&summary).Error; err != nil {
		return err
	}
	out.SummaryTokens = summary.SummaryTokens
	return nil
}

// TrendSeriesRow 是趋势图的读模型：一条 (provider, model) 序列在某一个时间桶里的 token 用量。
//
// Day 是桶键。桶按 UTC 切、读回来还是同一个 UTC 时刻 —— AGENTS 聚合口径要求
// 「桶键读回来必须还是同一时刻」，所以这里不用 ::date（那会把 timestamptz 隐式
// 转成本地日期，跨时区读回同一天会漂到前后一天）。
type TrendSeriesRow struct {
	ProviderKey string
	ModelID     string
	Day         time.Time
	Tokens      int64
}

func (m *SessionModel) SessionTokenTrendBySeries(ctx context.Context, f SessionQuery, since time.Time, trunc string) (rows []TrendSeriesRow, err error) {
	if trunc != "week" {
		trunc = "day"
	}
	// AT TIME ZONE 'UTC' 写两遍：第一遍把 timestamptz 落到 UTC 墙上时间再切桶，
	// 第二遍把 date_trunc 出来的无时区值盖回 UTC 时区，回来就是同一个时刻。
	err = m.events(ctx).
		Where("session_id IN (?)", applySessionQuery(m.sessions(ctx), f).Select("id")).
		Where("create_time >= ?", since).
		Select("provider_key, model_id, date_trunc('" + trunc + "', create_time AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS day, COALESCE(SUM(content_tokens), 0) AS tokens").
		Group("provider_key, model_id, day").Order("day ASC").Find(&rows).Error
	return rows, err
}

type SessionFacets struct {
	Providers []string
	Models    []string
	Creators  []int64
}

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

type SessionKindUsage struct {
	Kind   string
	Events int64
	Tokens int64
}

func (m *SessionModel) SessionKindUsageOf(ctx context.Context, sessionID int64) (rows []SessionKindUsage, err error) {
	err = m.events(ctx).
		Select("kind, COUNT(*) AS events, COALESCE(SUM(content_tokens), 0) AS tokens").
		Where("session_id = ?", sessionID).
		Group("kind").
		Order("MIN(seq) ASC").
		Scan(&rows).Error
	return rows, err
}

type SessionModelUsage struct {
	SessionID   int64
	ProviderKey string
	ModelID     string
	Events      int64
	Tokens      int64
}

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
