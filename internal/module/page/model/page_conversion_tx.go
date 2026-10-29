package pagemodel

import (
	"context"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LockDraftTx 把版本/hash 复核与随后的 revision 写入固定在同一行锁内。
func (m *Model) LockDraftTx(ctx context.Context, tx *gorm.DB, projectID, id string) (*PageEntity, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	var row PageEntity
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", id, projectID).Take(&row).Error
	return &row, err
}
