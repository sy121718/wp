package rls

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const SettingKey = "app.project_id"

const ScopedPredicate = "(project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)"

const GlobalPredicate = "(project_id IS NULL OR project_id = " +
	"NULLIF(current_setting('app.project_id', true), '')::uuid)"

var ErrInvalidProjectID = errors.New("rls: 工程 id 不是合法 uuid")

var ErrNotInTransaction = errors.New("rls: 会话变量必须在事务内设置（事务外会立即失效）")

func ScopeTx(tx *gorm.DB, projectID string) error {
	if tx == nil || tx.Statement == nil {
		return ErrNotInTransaction
	}

	if _, ok := tx.Statement.ConnPool.(*sql.Tx); !ok {
		return ErrNotInTransaction
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return ErrInvalidProjectID
	}
	return tx.Exec("SELECT set_config('"+SettingKey+"', ?, true)", projectID).Error
}

func BypassedRole(ctx context.Context, db *gorm.DB) (bool, error) {

	identity, err := ProbeIdentity(ctx, db)
	if err != nil {
		return false, err
	}
	return identity.BypassesRLS(), nil
}

func InProjectScope(ctx context.Context, db *gorm.DB, projectID string, fn func(tx *gorm.DB) error) error {
	if _, err := uuid.Parse(projectID); err != nil {
		return ErrInvalidProjectID
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}
