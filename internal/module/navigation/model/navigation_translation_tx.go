package navigationmodel

import (
	"context"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func (m *Model) ListTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (list []*NavigationEntity, err error) {
	if tx == nil {

		return nil, ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	q := tx.WithContext(ctx).Model(&NavigationEntity{}).Where("project_id = ?", projectID)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if err := q.Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if m == nil || m.db == nil {
		return ErrTransactionRequired
	}
	return m.db.WithContext(ctx).Transaction(fn)
}
