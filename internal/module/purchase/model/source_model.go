package purchasemodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type SourceEntity struct {
	ID           string          `gorm:"column:id;primaryKey"`
	ProjectID    string          `gorm:"column:project_id;not null"`
	Code         string          `gorm:"column:code;not null"`
	Name         string          `gorm:"column:name;not null"`
	Type         string          `gorm:"column:type;not null"`
	RelatedParty bool            `gorm:"column:related_party;not null"`
	SettlePrice  *float64        `gorm:"column:settle_price"`
	Status       string          `gorm:"column:status;not null"`
	Config       json.RawMessage `gorm:"column:config;type:jsonb;not null"`
	Sort         int             `gorm:"column:sort;not null"`
	Metadata     json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt    time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt    time.Time       `gorm:"column:update_time;not null"`
}

func (SourceEntity) TableName() string { return "inventory_sources" }

type SourceFilter struct {
	ProjectID    string
	Type         string
	RelatedParty *bool
	Status       string
	Keyword      string

	HasSettlePrice *bool
}

type SourceSummaryRow struct {
	Type         string `gorm:"column:type"`
	RelatedParty bool   `gorm:"column:related_party"`
	Count        int64  `gorm:"column:count"`
}

func (m *Model) SourceDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SourceEntity{})
}

func (m *Model) CreateSource(ctx context.Context, e *SourceEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&SourceEntity{}).Create(e).Error
	})
}

func (m *Model) GetSource(ctx context.Context, id, projectID string) (e *SourceEntity, err error) {
	e = &SourceEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&SourceEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) SourceCodeExists(ctx context.Context, projectID, code, excludeID string) (exists bool, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&SourceEntity{}).
			Where("project_id = ? AND upper(code) = upper(?)", projectID, code)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if cerr := q.Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	})
	return exists, err
}

func applySourceFilter(q *gorm.DB, f SourceFilter) *gorm.DB {
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.RelatedParty != nil {
		q = q.Where("related_party = ?", *f.RelatedParty)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		q = q.Where("name ILIKE ? OR code ILIKE ?", like, like)
	}
	if f.HasSettlePrice != nil {
		if *f.HasSettlePrice {
			q = q.Where("settle_price IS NOT NULL")
		} else {
			q = q.Where("settle_price IS NULL")
		}
	}
	return q
}

func (m *Model) ListSources(ctx context.Context, f SourceFilter, limit, offset int) (list []*SourceEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := applySourceFilter(tx.WithContext(ctx).Model(&SourceEntity{}), f).Order("sort ASC, code ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) CountSources(ctx context.Context, f SourceFilter) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return applySourceFilter(tx.WithContext(ctx).Model(&SourceEntity{}), f).Count(&n).Error
	})
	return n, err
}

func (m *Model) SummarySources(ctx context.Context, projectID string) (rows []*SourceSummaryRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&SourceEntity{}).
			Select("type, related_party, COUNT(*) AS count").
			Where("project_id = ?", projectID).
			Group("type, related_party").
			Order("type ASC, related_party DESC").
			Scan(&rows).Error
	})
	return rows, err
}

func (m *Model) UpdateSource(ctx context.Context, e *SourceEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&SourceEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

func (m *Model) DeleteSource(ctx context.Context, id string) (err error) {
	return m.SourceDB(ctx).Where("id = ?", id).Delete(&SourceEntity{}).Error
}

func (m *Model) CreateSourceTx(ctx context.Context, tx *gorm.DB, e *SourceEntity) error {
	return tx.WithContext(ctx).Model(&SourceEntity{}).Create(e).Error
}

func (m *Model) LockSourceTx(ctx context.Context, tx *gorm.DB, id, projectID string) (*SourceEntity, error) {
	var e SourceEntity
	err := tx.WithContext(ctx).Model(&SourceEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ?", id, projectID).First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *Model) UpdateSourceTx(ctx context.Context, tx *gorm.DB, e *SourceEntity) error {
	return tx.WithContext(ctx).Model(&SourceEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteSourceTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&SourceEntity{}).Where("id = ?", id).Delete(&SourceEntity{}).Error
}
