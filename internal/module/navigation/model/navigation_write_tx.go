package navigationmodel

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

func (m *Model) CreateTx(ctx context.Context, tx *gorm.DB, e *NavigationEntity) error {
	if tx == nil {
		return ErrTransactionRequired
	}
	if e == nil {
		return errors.New("navigationmodel: CreateTx 需要实体")
	}
	if err := rls.ScopeTx(tx, e.ProjectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&NavigationEntity{}).Create(e).Error
}

func (m *Model) GetForUpdateTx(ctx context.Context, tx *gorm.DB, projectID, id string) (*NavigationEntity, error) {
	if tx == nil {
		return nil, ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	var e NavigationEntity
	err := tx.WithContext(ctx).Model(&NavigationEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ?", id, projectID).
		Take(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *Model) SaveTx(ctx context.Context, tx *gorm.DB, projectID, id string, updates map[string]any) error {
	if tx == nil {
		return ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&NavigationEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).Updates(updates).Error
}

func (m *Model) SaveWithExpectedTx(ctx context.Context, tx *gorm.DB, projectID, id string,
	updates map[string]any, expected time.Time) (rows int64, err error) {
	if tx == nil {
		return 0, ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	res := tx.WithContext(ctx).Model(&NavigationEntity{}).
		Where("id = ? AND update_time = ? AND project_id = ?", id, expected, projectID).
		Updates(updates)
	return res.RowsAffected, res.Error
}

func (m *Model) DeleteManyTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) error {
	if tx == nil {
		return ErrTransactionRequired
	}
	if len(ids) == 0 {
		return nil
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&NavigationEntity{}).
		Where("project_id = ? AND id IN ?", projectID, ids).Delete(&NavigationEntity{}).Error
}

func (m *Model) SwapSortOrdersTx(ctx context.Context, tx *gorm.DB, projectID string, first, second SwapRow) error {
	if tx == nil {
		return ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	rows := []SwapRow{first, second}
	if rows[0].ID > rows[1].ID {
		rows[0], rows[1] = rows[1], rows[0]
	}
	now := time.Now().UTC()
	for _, row := range rows {
		res := tx.WithContext(ctx).Model(&NavigationEntity{}).
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
}

func (m *Model) MaxSortOrderTx(ctx context.Context, tx *gorm.DB, projectID, kind string, parentID *string) (maxOrder int, err error) {
	if tx == nil {
		return 0, ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return 0, err
	}
	q := tx.WithContext(ctx).Model(&NavigationEntity{}).Where("project_id = ? AND kind = ?", projectID, kind)
	if parentID == nil || *parentID == "" {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	if err := q.Select("COALESCE(MAX(sort_order), 0)").Row().Scan(&maxOrder); err != nil {
		return 0, err
	}
	return maxOrder, nil
}

func (m *Model) ExistsPathTx(ctx context.Context, tx *gorm.DB, projectID, kind, path, excludeID string) (bool, error) {
	if tx == nil {
		return false, ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return false, err
	}
	var count int64
	q := tx.WithContext(ctx).Model(&NavigationEntity{}).
		Where("project_id = ? AND kind = ? AND path = ?", projectID, kind, path)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
