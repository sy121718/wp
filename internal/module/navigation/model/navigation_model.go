package navigationmodel

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNameNavigations = "navigations"

type NavigationEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Title     string `gorm:"column:title;not null"`
	Path      string `gorm:"column:path;not null"`
	Kind      string `gorm:"column:kind;not null"`

	SourceType string  `gorm:"column:source_type;not null;default:custom"`
	SourceID   *string `gorm:"column:source_id"`

	Target string `gorm:"column:target;not null;default:self"`

	PanelBlockID *string `gorm:"column:panel_block_id"`

	PanelWidth string    `gorm:"column:panel_width;not null;default:auto"`
	ParentID   *string   `gorm:"column:parent_id"`
	SortOrder  int       `gorm:"column:sort_order;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time `gorm:"column:update_time;not null"`
}

func (NavigationEntity) TableName() string { return tableNameNavigations }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&NavigationEntity{})
}

func (m *Model) Create(ctx context.Context, e *NavigationEntity) error {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).Create(e).Error
	})
}

func (m *Model) Get(ctx context.Context, projectID, id string) (e *NavigationEntity, err error) {
	var row NavigationEntity
	if strings.TrimSpace(projectID) == "" {
		if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
			return nil, err
		}
		return &row, nil
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(&row).Error
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) List(ctx context.Context, projectID, kind string) (list []*NavigationEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&NavigationEntity{}).Where("project_id = ?", projectID)
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		return q.Order("sort_order ASC, id ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) MaxSortOrder(ctx context.Context, projectID, kind string, parentID *string) (maxOrder int, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&NavigationEntity{}).Where("project_id = ? AND kind = ?", projectID, kind)
		if parentID == nil || *parentID == "" {
			q = q.Where("parent_id IS NULL")
		} else {
			q = q.Where("parent_id = ?", *parentID)
		}
		return q.Select("COALESCE(MAX(sort_order), 0)").Row().Scan(&maxOrder)
	})
	return maxOrder, err
}

func (m *Model) Save(ctx context.Context, projectID, id string, updates map[string]any) error {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Updates(updates).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).Updates(updates).Error
	})
}

func (m *Model) SaveWithExpected(ctx context.Context, projectID, id string,
	updates map[string]any, expected time.Time) (rows int64, err error) {
	build := func(db *gorm.DB) *gorm.DB {
		q := db.Model(&NavigationEntity{}).
			Where("id = ? AND update_time = ?", id, expected)
		if strings.TrimSpace(projectID) != "" {
			q = q.Where("project_id = ?", projectID)
		}
		return q
	}
	if strings.TrimSpace(projectID) == "" {
		res := build(m.db.WithContext(ctx)).Updates(updates)
		return res.RowsAffected, res.Error
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res := build(tx).Updates(updates)
		rows = res.RowsAffected
		return res.Error
	})
	return rows, err
}

type SwapRow struct {
	ID        string
	SortOrder int
	Expected  time.Time
}

type SwapConflictError struct {
	ID string
}

func (e *SwapConflictError) Error() string {
	return "navigation: swap conflict on row " + e.ID
}

func (m *Model) SwapSortOrders(ctx context.Context, projectID string, first, second SwapRow) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("navigation: swap requires project scope")
	}
	rows := []SwapRow{first, second}
	if rows[0].ID > rows[1].ID {
		rows[0], rows[1] = rows[1], rows[0]
	}
	now := time.Now().UTC()
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		for _, row := range rows {
			res := tx.Model(&NavigationEntity{}).
				Where("id = ? AND project_id = ? AND update_time = ?", row.ID, projectID, row.Expected).
				Updates(map[string]any{"sort_order": row.SortOrder, "update_time": now})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return &SwapConflictError{ID: row.ID}
			}
		}
		return nil
	})
}

func (m *Model) Delete(ctx context.Context, projectID, id string) error {
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id = ?", id).Delete(&NavigationEntity{}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).Delete(&NavigationEntity{}).Error
	})
}

func (m *Model) DeleteMany(ctx context.Context, projectID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if strings.TrimSpace(projectID) == "" {
		return m.DB(ctx).Where("id IN ?", ids).Delete(&NavigationEntity{}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&NavigationEntity{}).
			Where("project_id = ? AND id IN ?", projectID, ids).Delete(&NavigationEntity{}).Error
	})
}

func (m *Model) ExistsPath(ctx context.Context, projectID, kind, path, excludeID string) (bool, error) {
	var count int64
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&NavigationEntity{}).Where("project_id = ? AND kind = ? AND path = ?", projectID, kind, path)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		return q.Count(&count).Error
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
