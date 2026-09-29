// membership_tier_model.go — 等级表（membership_tiers）的仓储。
//
// 两套方法的分工（与 page/model/page_tx.go 同一条判据）：
//
//	· *Tx 变体 —— 只在**调用方给的事务句柄**上执行，SQL 唯一真源在本文件；
//	· 非 Tx 变体 —— 薄包装：自己开一个工程作用域事务再调 *Tx 版本。
//
// 为什么不让非 Tx 版本各写一遍 SQL：改动时漏掉一处就会出现「两个入口行为不同」，
// 而这种分叉在「事务/作用域都在」时看不出来（只在某条路径上悄悄少了个条件）。
package membershipmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// TierEntity membership_tiers 的一行。
//
// 不声明列型（internal/architecture/model_gorm_tag_test.go 守门）：全部是 gorm 能推断的
// 基础类型与 time.Time，没有 json.RawMessage 那类需要显式 type: 的列。
type TierEntity struct {
	ID        int64  `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id"`
	Name      string `gorm:"column:name"`
	// SortOrder 等级高低，越大越高。
	SortOrder int `gorm:"column:sort_order"`
	// ThresholdAmount 升级门槛，单位**分**（与 orders 的金额列同口径）。
	ThresholdAmount int64      `gorm:"column:threshold_amount"`
	IsDefault       bool       `gorm:"column:is_default"`
	Remark          string     `gorm:"column:remark"`
	CreateTime      time.Time  `gorm:"column:create_time"`
	UpdateTime      time.Time  `gorm:"column:update_time"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
}

// TableName 显式绑定表名（不靠 gorm 的复数化推断）。
func (TierEntity) TableName() string { return "membership_tiers" }

// ListTiers 列出某工程的等级（sort_order 降序、同序按 id 升序 —— 与解析路径同序，
// 后台列表的顺序因此与「谁更高」一致）。
func (m *Model) ListTiers(ctx context.Context, projectID string) (list []*TierEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		list, ierr = m.ListTiersTx(ctx, tx, projectID)
		return ierr
	})
	return list, err
}

// ListTiersTx 在调用方事务内列出某工程的等级。
func (m *Model) ListTiersTx(ctx context.Context, tx *gorm.DB, projectID string) (list []*TierEntity, err error) {
	err = tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Order("sort_order DESC, id ASC").
		Find(&list).Error
	return list, err
}

// GetTier 按 id 取某工程的等级。
//
// 作用域是必须的：策略是行级的，不设 app.project_id 时 `WHERE id = ?` 也一行都看不见 ——
// 因此「拿别人的 tier_id 来读写」在数据库层就被挡住，不需要 service 再判一次归属。
func (m *Model) GetTier(ctx context.Context, projectID string, tierID int64) (e *TierEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		e, ierr = m.GetTierTx(ctx, tx, projectID, tierID)
		return ierr
	})
	return e, err
}

// GetTierTx 在调用方事务内按 id 取某工程的等级。
func (m *Model) GetTierTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (e *TierEntity, err error) {
	var row TierEntity
	if err = tx.WithContext(ctx).Model(&TierEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", tierID, projectID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetDefaultTier 取某工程的默认等级（每工程恰一个，由部分唯一索引 uq_membership_tiers_default 保证）。
//
// 找不到时返回 gorm.ErrRecordNotFound —— service 把它翻译成 enums.ErrDefaultTierMissing
// 并带上 project_id（**不**退回 sort_order 最小的那一档：那一档可能是运营已停用的）。
func (m *Model) GetDefaultTier(ctx context.Context, projectID string) (e *TierEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		e, ierr = m.GetDefaultTierTx(ctx, tx, projectID)
		return ierr
	})
	return e, err
}

// GetDefaultTierTx 在调用方事务内取某工程的默认等级。
func (m *Model) GetDefaultTierTx(ctx context.Context, tx *gorm.DB, projectID string) (e *TierEntity, err error) {
	var row TierEntity
	if err = tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND is_default AND deleted_at IS NULL", projectID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// CreateTierTx 在调用方事务内新建等级。
//
// 只提供 Tx 变体：新建等级与「同批写权益」是一个用户可感知的写操作（两处持久化写入），
// 必须同一事务 —— 只有非 Tx 版本就会出现「等级建好了、权益没写进去」的半截状态。
func (m *Model) CreateTierTx(ctx context.Context, tx *gorm.DB, e *TierEntity) error {
	return tx.WithContext(ctx).Model(&TierEntity{}).Create(e).Error
}

// UpdateTierFieldsTx 在事务内按字段映射更新等级（只更新传入的列）。
//
// 值以 map 传入而不是结构体：调用方（service）已按「指针字段 = 只在非 nil 时改」的语义
// 组装好差异集，这里不重复做「零值到底是要改还是不改」的判断 —— 那个判断在两层各写一遍
// 必然分叉（`sort_order = 0` 是合法取值，不能用零值判「未传」）。
func (m *Model) UpdateTierFieldsTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, fields map[string]any) (int64, error) {
	if len(fields) == 0 {
		return 0, nil
	}
	fields["update_time"] = time.Now()
	res := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", tierID, projectID).
		Updates(fields)
	return res.RowsAffected, res.Error
}

// SoftDeleteTierTx 在事务内软删等级（不真删：归属与历史要能按 id 反查）。
func (m *Model) SoftDeleteTierTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (int64, error) {
	res := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", tierID, projectID).
		Updates(map[string]any{"deleted_at": time.Now(), "update_time": time.Now()})
	return res.RowsAffected, res.Error
}

// CountByThresholdTx 统计某工程用了同一门槛的**非默认**等级数（不含 excludeID 自己）。
//
// 门槛撞车要在应用层先判一次，理由是给出可行动的说法（「黄金已经是 1000 的门槛了」）；
// 数据库的部分唯一索引 uq_membership_tiers_threshold 仍是最后那道
// （脚本 / 手工 SQL 也绕不过）—— 两者不是替代关系。
func (m *Model) CountByThresholdTx(ctx context.Context, tx *gorm.DB, projectID string, threshold int64, excludeID int64) (int64, error) {
	var n int64
	q := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND threshold_amount = ? AND is_default = FALSE AND deleted_at IS NULL", projectID, threshold)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}

// ClearDefaultTx 取消某工程当前的默认标记（不含 excludeID）。
//
// 「切换默认等级」是一次用户可感知的写操作里的两步（取消旧的 + 设新的），
// 必须在同一事务内 —— 分两次而失败一半会得到「一个默认等级都没有」的工程，
// 读路径随即对所有访客报 ErrDefaultTierMissing。部分唯一索引 uq_membership_tiers_default
// 同时保证顺序错误（先设新再取消旧）会当场撞 23505，而不是留下两个默认等级。
func (m *Model) ClearDefaultTx(ctx context.Context, tx *gorm.DB, projectID string, excludeID int64) (int64, error) {
	res := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND is_default AND deleted_at IS NULL AND id <> ?", projectID, excludeID).
		Updates(map[string]any{"is_default": false, "update_time": time.Now()})
	return res.RowsAffected, res.Error
}

// CountByNameTx 统计某工程用了同一名称的等级数（不含 excludeID 自己）。
func (m *Model) CountByNameTx(ctx context.Context, tx *gorm.DB, projectID, name string, excludeID int64) (int64, error) {
	var n int64
	q := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND name = ? AND deleted_at IS NULL", projectID, name)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}

// CountDefaultTx 统计某工程的默认等级数（不含 excludeID 自己）。
func (m *Model) CountDefaultTx(ctx context.Context, tx *gorm.DB, projectID string, excludeID int64) (int64, error) {
	var n int64
	q := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND is_default AND deleted_at IS NULL", projectID)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}
