package productmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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

func (OutboxEventEntity) TableName() string { return "product_outbox_events" }

func (m *Model) LockEntityTx(ctx context.Context, tx *gorm.DB, key string) error {
	return tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", key).Error
}

func (m *Model) NextOutboxRevisionTx(ctx context.Context, tx *gorm.DB, entityType, entityID string) (int64, error) {

	if err := m.LockEntityTx(ctx, tx, entityType+":"+entityID); err != nil {
		return 0, err
	}
	var next int64
	if err := tx.WithContext(ctx).Model(&OutboxEventEntity{}).
		Select("COALESCE(MAX(entity_revision), 0) + 1").
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Scan(&next).Error; err != nil {
		return 0, err
	}
	return next, nil
}

func (m *Model) AppendOutboxTx(ctx context.Context, tx *gorm.DB, rows []OutboxEventEntity) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(rows, 200).Error
}

func (m *Model) ClaimPendingOutbox(ctx context.Context, lease time.Duration, limit int) (rows []OutboxEventEntity, err error) {
	if limit <= 0 {
		return nil, nil
	}
	secs := int64(lease / time.Second)
	if secs <= 0 {
		secs = 1
	}

	var claimed []OutboxEventEntity
	candidates := m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Select("id").
		Where("processed_time IS NULL").
		Where("claimed_time IS NULL OR claimed_time < now() - (? * interval '1 second')", secs).
		Order("id").
		Limit(limit).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	err = m.db.WithContext(ctx).Model(&claimed).
		Where("id IN (?)", candidates).
		Clauses(clause.Returning{}).
		Updates(map[string]any{
			"claimed_time": gorm.Expr("now()"),
			"attempts":     gorm.Expr("attempts + 1"),
		}).Error
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (m *Model) MarkOutboxProcessed(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	return m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"processed_time": now, "last_error": ""}).Error
}

func (m *Model) MarkOutboxFailed(ctx context.Context, id int64, msg string) error {
	if id <= 0 {
		return nil
	}
	return m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Where("id = ?", id).
		Update("last_error", msg).Error
}

func (m *Model) CountPendingOutbox(ctx context.Context) (n int64, err error) {
	err = m.db.WithContext(ctx).Model(&OutboxEventEntity{}).
		Where("processed_time IS NULL").Count(&n).Error
	return n, err
}

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
