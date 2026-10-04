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
	ID             int64     `gorm:"column:id;primaryKey"`
	SessionID      int64     `gorm:"column:session_id"`
	Seq            int64     `gorm:"column:seq"`
	Kind           string    `gorm:"column:kind"`
	SurfaceOp      string    `gorm:"column:surface_op"`
	ReplaceFromSeq int64     `gorm:"column:replace_from_seq"`
	ReplaceToSeq   int64     `gorm:"column:replace_to_seq"`
	Content        string    `gorm:"column:content"`
	ContentTokens  int64     `gorm:"column:content_tokens"`
	Meta           JSONMap   `gorm:"column:meta;type:jsonb"`
	CreateTime     time.Time `gorm:"column:create_time;autoCreateTime"`
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
