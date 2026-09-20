package productmodel

// product_outbox_model.go — 商品写路径的静态产物失效 outbox（审计 ARCH-01）。
//
// 表由迁移 309 建立；本文件只做本模块表的 CRUD。**刻意不装 RLS 策略**（与 build_jobs
// 同一情形）：它是按状态跨工程捞取的队列，消费者要一次领取所有工程的待办，
// 装了 FORCE 策略后未设 app.project_id 的领取会静默 0 行。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// OutboxEventEntity 一条待消费的失效事件（一行 = 一个依赖键）。
//
// 同一实体的一次变更写多行（不同依赖键），共享 entity_type / entity_id /
// entity_revision —— 「这次变更影响了哪些依赖源」因此是库里可查的事实。
type OutboxEventEntity struct {
	ID             int64      `gorm:"column:id;primaryKey"`
	ProjectID      string     `gorm:"column:project_id;not null"`
	EntityType     string     `gorm:"column:entity_type;not null"`
	EntityID       string     `gorm:"column:entity_id;not null"`
	EntityRevision int64      `gorm:"column:entity_revision;not null"`
	DependencyKind string     `gorm:"column:dependency_kind;not null"`
	DependencyKey  string     `gorm:"column:dependency_key;not null"`
	CreateTime     time.Time  `gorm:"column:create_time;not null"`
	ClaimedTime    *time.Time `gorm:"column:claimed_time"`
	ProcessedTime  *time.Time `gorm:"column:processed_time"`
	Attempts       int        `gorm:"column:attempts;not null"`
	LastError      string     `gorm:"column:last_error;not null"`
}

// TableName 表名（与迁移 309 一致）。
func (OutboxEventEntity) TableName() string { return "product_outbox_events" }

// NextOutboxRevisionTx 取该实体的下一次变更版本（同实体单调递增）。
//
// 事务级顾问锁的用途：两个并发事务同时改同一实体时，MAX+1 会算出同一个版本号。
// 版本号本身只用于诊断与「这条事件属于第几次变更」的表述，但既然它叫 revision，
// 就该是**同实体单调**的 —— 靠 advisory lock 把它做成真的，比留一个「大部分时候
// 正确」的计数器便宜。（锁在事务提交/回滚时自动释放，不需要手工清理。）
func (m *Model) NextOutboxRevisionTx(ctx context.Context, tx *gorm.DB, entityType, entityID string) (int64, error) {
	// 锁键取「实体类型:实体 id」的哈希：不同实体互不阻塞，同一实体串行。
	if err := tx.WithContext(ctx).Exec(
		"SELECT pg_advisory_xact_lock(hashtext(?))", entityType+":"+entityID).Error; err != nil {
		return 0, err
	}
	var next int64
	if err := tx.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(entity_revision), 0) + 1 FROM product_outbox_events "+
			"WHERE entity_type = ? AND entity_id = ?", entityType, entityID).Scan(&next).Error; err != nil {
		return 0, err
	}
	return next, nil
}

// AppendOutboxTx 在调用方事务内追加事件行（批次写入）。
func (m *Model) AppendOutboxTx(ctx context.Context, tx *gorm.DB, rows []OutboxEventEntity) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(rows, 200).Error
}

// ClaimPendingOutbox 领取一批未处理事件（租约 + SKIP LOCKED）。
//
// 语义是**至少一次**：领取只推进 claimed_time / attempts，不写 processed_time；
// 消费者崩溃后租约到期，同一批会被下一个消费者重新领取。失效动作本身幂等
// （标记 stale 是幂等的 UPDATE、重建按当前数据重算），重放不会产生副作用累积。
func (m *Model) ClaimPendingOutbox(ctx context.Context, lease time.Duration, limit int) (rows []OutboxEventEntity, err error) {
	if limit <= 0 {
		return nil, nil
	}
	secs := int64(lease / time.Second)
	if secs <= 0 {
		secs = 1
	}
	err = m.db.WithContext(ctx).Raw(`
		UPDATE product_outbox_events e
		   SET claimed_time = now(), attempts = e.attempts + 1
		 WHERE e.id IN (
		       SELECT id FROM product_outbox_events
		        WHERE processed_time IS NULL
		          AND (claimed_time IS NULL OR claimed_time < now() - (? * interval '1 second'))
		        ORDER BY id
		        LIMIT ?
		        FOR UPDATE SKIP LOCKED
		 )
		RETURNING e.id, e.project_id, e.entity_type, e.entity_id, e.entity_revision,
		          e.dependency_kind, e.dependency_key, e.create_time, e.claimed_time,
		          e.processed_time, e.attempts, e.last_error`, secs, limit).Scan(&rows).Error
	return rows, err
}

// MarkOutboxProcessed 把已成功消费的事件标记为处理完成。
func (m *Model) MarkOutboxProcessed(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	return m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"processed_time": now, "last_error": ""}).Error
}

// MarkOutboxFailed 记录一次消费失败（保留待重试；租约到期后可被重新领取）。
func (m *Model) MarkOutboxFailed(ctx context.Context, id int64, msg string) error {
	if id <= 0 {
		return nil
	}
	return m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Where("id = ?", id).
		Update("last_error", msg).Error
}

// CountPendingOutbox 未处理事件条数（诊断与回归用）。
func (m *Model) CountPendingOutbox(ctx context.Context) (n int64, err error) {
	err = m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Where("processed_time IS NULL").Count(&n).Error
	return n, err
}

// ListOutboxEvents 列出事件行（测试与诊断用；entityType / entityID 为空表示不过滤）。
func (m *Model) ListOutboxEvents(ctx context.Context, entityType, entityID string) (rows []OutboxEventEntity, err error) {
	q := m.db.WithContext(ctx).Model(&OutboxEventEntity{}).Order("id")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if entityID != "" {
		q = q.Where("entity_id = ?", entityID)
	}
	err = q.Find(&rows).Error
	return rows, err
}
