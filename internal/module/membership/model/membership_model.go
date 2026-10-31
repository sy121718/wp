package membershipmodel

import (
	"context"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&TierEntity{})
}

func (m *Model) EntitlementDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&EntitlementEntity{})
}

func (m *Model) AssignmentDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AssignmentEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}
