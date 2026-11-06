package pagemodel

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// TransactionAny 提供一个不带工程作用域的事务句柄：页面批量删除可能跨工程，
// 需要在一个事务里按工程逐段切换 RLS 作用域，避免逐工程独立提交造成半成功。
func (m *Model) TransactionAny(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if fn == nil {
		return errors.New("page: 批量删除事务必须给出回调")
	}
	return m.db.WithContext(ctx).Transaction(fn)
}

// ScopeTx 在调用方事务内切换工程作用域；RLS 会话变量是事务级的，可多次覆盖。
func (m *Model) ScopeTx(tx *gorm.DB, projectID string) error {
	return rls.ScopeTx(tx, projectID)
}

// LockPagesByIDsTx 在调用方事务内按 id 升序锁定同一工程内仍存活的页面行。
// 排序固定，避免两个批量以相反顺序取行锁形成死锁。
func (m *Model) LockPagesByIDsTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) (list []PageEntity, err error) {
	if tx == nil {
		return nil, errors.New("page: 页面行锁必须在调用方事务内取")
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&PageEntity{}).
		Select("id", "project_id", "draft_path", "input_version").
		Where("project_id = ? AND id IN ? AND deleted_at IS NULL", projectID, ids).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&list).Error
	return list, err
}

// PageExistsTx 在调用方事务内确认页面仍存活且属于该工程。
//
// 引用写入方（内容模板、自动发布实例等）在与删除方同一把工程内容写锁下调用本方法，
// 才能挡住「删除先提交、写入方随后写入悬空引用」。它只读页面存在性，
// 不带出文档，避免把页面内容拖进别人的写事务。
func (m *Model) PageExistsTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) (bool, error) {
	if tx == nil {
		return false, errors.New("page: 页面存在性复核必须在调用方事务内")
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(pageID) == "" {
		return false, ErrProjectRequired
	}
	var count int64
	err := tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
