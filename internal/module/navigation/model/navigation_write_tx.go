package navigationmodel

// navigation_write_tx.go — 导航写入口需要的**事务内**变体（与自动提交的 Create / Save /
// SaveWithExpected / SwapSortOrders 逐字同义，只换连接与作用域）。
//
// 为什么需要：译文写入（service 的 TranslationWriter）与菜单 CRUD 必须落在**同一个**
// 工程级 advisor 锁下，否则「同 hash 新增位置」这类并发会让 write 锁内读到的来源是
// 提交前快照、新增的位置一条 stale 都不派发。锁是事务级的（pg_advisory_xact_lock），
// 只有把 CRUD 的写语句也搬进那个事务，锁才真正覆盖它。
//
// 判据（与 navigation_translation_tx.go 的两条入口同形）：
//   - 没有事务句柄 → ErrTransactionRequired（绝不偷偷退回 m.db：那是另一份快照，
//     调用方会以为自己读的是锁内的数据）；
//   - 作用域在传入的 tx 上用 rls.ScopeTx 设；
//   - SQL 列集与 WHERE 条件与自动提交版逐字相同，差别只有连接。

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// CreateTx 在调用方事务内新增导航项。
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

// GetForUpdateTx 在调用方事务内按 ID 重读当前行并加行锁（SELECT ... FOR UPDATE）。
//
// 为什么必须锁内重读：写入口的定位在锁外（请求只带 id，要先逐工程探测出归属）。
// 从「拿到实体」到「取到工程内容写锁」之间，另一个写者可能已经改过这一行的
// title / path / kind / parent。若继续用锁外那份快照做校验与失效，会出现两类静默错误：
// 用**过期的 kind** 去查路径唯一性和父引用（校验了错误的维度），以及只对**过期的旧
// kind** 打失效（真正被改掉的那个位置一条 stale 都不派发）。行锁同时把「同一行的两个
// 并发 Update」串起来，让基于当前值的判定不再建立在快照上。
//
// 不存在 → gorm.ErrRecordNotFound（由 service 归一成 ErrNotFound / 冲突文案）。
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

// SaveTx 在调用方事务内按 ID 部分更新（Where("id = ? AND project_id = ?").Updates(map)）。
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

// SaveWithExpectedTx 在调用方事务内做条件更新（乐观锁路径），返回受影响行数。
//
// 与 SaveWithExpected 同一份 WHERE（id + update_time [+ project_id]）：affected == 0
// 即「版本已变 / 行没了 / 越界」，由 service 区分（实体已定位过，所以只可能是版本已变）。
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

// DeleteManyTx 在调用方事务内批量删除导航项（同一聚合：菜单项及其全部子孙）。
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

// SwapSortOrdersTx 在调用方事务内交换两行的 sort_order。
//
// 执行顺序与每条「必须恰好 1 行」的判据与 SwapSortOrders 逐字相同（按 id 升序防死锁、
// 任一 != 1 即 SwapConflictError）；本函数**不回滚**：回滚是调用方事务的职责，
// 错误返回即由外层 Transaction 整体回滚。
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

// MaxSortOrderTx 在调用方事务内取同工程同 kind 同父级下的最大排序值（无记录返回 0）。
//
// 与 MaxSortOrder 同一条 SQL；Create 的「追加到同级末尾」必须在锁内取号，否则两个并发
// Create 会拿到同一个 max+1（同级排序并列，管理页的上移/下移从此不稳定）。
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

// ExistsPathTx 在调用方事务内判断同工程同 kind 下 path 是否被其他项占用。
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
