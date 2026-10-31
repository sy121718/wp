package masterdatamodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const TableMasterDataChanges = "master_data_changes"

type ChangeEntity struct {
	ID string `gorm:"column:id;primaryKey"`

	ProjectID string `gorm:"column:project_id;not null"`

	EntityType string `gorm:"column:entity_type;not null"`
	EntityID   string `gorm:"column:entity_id;not null"`

	EntityLabel string `gorm:"column:entity_label;not null"`
	Action      string `gorm:"column:action;not null"`
	Field       string `gorm:"column:field;not null"`
	OldValue    string `gorm:"column:old_value;not null"`
	NewValue    string `gorm:"column:new_value;not null"`

	Origin string `gorm:"column:origin;not null"`

	OperatorID string    `gorm:"column:operator_id;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
}

func (ChangeEntity) TableName() string { return TableMasterDataChanges }

type ChangeFilter struct {
	ProjectID  string
	EntityType string
	EntityID   string
	Field      string
	Action     string

	Keyword    string
	OperatorID string

	Since *time.Time
	Until *time.Time
}

type EntityHistoryRow struct {
	EntityType     string    `gorm:"column:entity_type"`
	EntityID       string    `gorm:"column:entity_id"`
	EntityLabel    string    `gorm:"column:entity_label"`
	ChangeCount    int64     `gorm:"column:change_count"`
	LastAction     string    `gorm:"column:last_action"`
	LastField      string    `gorm:"column:last_field"`
	LastOperatorID string    `gorm:"column:last_operator_id"`
	LastAt         time.Time `gorm:"column:last_at"`
}

type Model struct{ db *gorm.DB }

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ChangeEntity{})
}

func (m *Model) Append(ctx context.Context, rows []*ChangeEntity) (err error) {
	if len(rows) == 0 {
		return nil
	}

	return rls.InProjectScope(ctx, m.db, rows[0].ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ChangeEntity{}).Create(rows).Error
	})
}

func (m *Model) AppendTx(tx *gorm.DB, rows []*ChangeEntity) (err error) {
	if len(rows) == 0 || tx == nil {
		return nil
	}

	if serr := rls.ScopeTx(tx, rows[0].ProjectID); serr != nil {
		return serr
	}
	return tx.Create(rows).Error
}

func applyChangeFilter(q *gorm.DB, f ChangeFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.EntityID != "" {
		q = q.Where("entity_id = ?", f.EntityID)
	}
	if f.Field != "" {
		q = q.Where("field = ?", f.Field)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.OperatorID != "" {
		q = q.Where("operator_id = ?", f.OperatorID)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("entity_label ILIKE ?", like)
	}
	if f.Since != nil {
		q = q.Where("create_time >= ?", *f.Since)
	}
	if f.Until != nil {
		q = q.Where("create_time < ?", *f.Until)
	}
	return q
}

func (m *Model) List(ctx context.Context, f ChangeFilter, limit, offset int) (list []*ChangeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applyChangeFilter(tx.Model(&ChangeEntity{}), f).Order("create_time DESC, id ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) Count(ctx context.Context, f ChangeFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyChangeFilter(tx.Model(&ChangeEntity{}), f).Count(&n).Error
	})
	return n, err
}

func (m *Model) ListEntityHistories(ctx context.Context, f ChangeFilter, limit, offset int) (list []*EntityHistoryRow, err error) {
	var q *gorm.DB
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q = applyChangeFilter(tx.Model(&ChangeEntity{}), f).
			Select("entity_type, entity_id, " +
				"(array_agg(entity_label ORDER BY create_time DESC, id ASC))[1] AS entity_label, " +
				"COUNT(*) AS change_count, " +
				"(array_agg(action ORDER BY create_time DESC, id ASC))[1] AS last_action, " +
				"(array_agg(field ORDER BY create_time DESC, id ASC))[1] AS last_field, " +
				"(array_agg(operator_id ORDER BY create_time DESC, id ASC))[1] AS last_operator_id, " +
				"MAX(create_time) AS last_at").
			Group("entity_type, entity_id").
			Order("last_at DESC, entity_type ASC, entity_id ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Scan(&list).Error
	})
	return list, err
}

func (m *Model) CountEntities(ctx context.Context, f ChangeFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applyChangeFilter(tx.Model(&ChangeEntity{}), f).
			Select("COUNT(DISTINCT (entity_type, entity_id)) AS count").
			Scan(&n).Error
	})
	return n, err
}
