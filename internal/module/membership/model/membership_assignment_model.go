// membership_assignment_model.go — 归属表（membership_assignments）的仓储。
//
// 本文件承载模块里最关键的一条不变量：**手工指定不被自动重算覆盖**。
// 它的实现方式是把守卫写进 SQL 的 WHERE 而不是让调用方记得跳过 ——
// 后者会在新增一个调用点（例如将来的批量重算、后台「重算全部」按钮）时静默失效，
// 而失效的表现是「运营手工调的等级第二天自己变回去了」，没有任何报错。
package membershipmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 归属来源常量（与迁移 462 的 ck_membership_assignments_source 一一对应）。
//
// 与 membershipenums.SourceAuto / SourceManual 同值：model 层不 import enums
// （依赖方向是 enum → 对外文案，model 只管持久化），两处值由 CHECK 约束与
// 一条单测（TestSourceConstantsMatchEnums）同时对账。
const (
	SourceAuto   = "auto"
	SourceManual = "manual"
)

// AssignmentEntity membership_assignments 的一行。
type AssignmentEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	ProjectID  string    `gorm:"column:project_id"`
	UserID     uint64    `gorm:"column:user_id"`
	TierID     int64     `gorm:"column:tier_id"`
	Source     string    `gorm:"column:source"`
	AssignedAt time.Time `gorm:"column:assigned_at"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

// TableName 显式绑定表名。
func (AssignmentEntity) TableName() string { return "membership_assignments" }

// GetAssignment 取某访客在某工程的当前归属（读路径的第一跳）。
//
// 读路径**只读**：查不到就返回 gorm.ErrRecordNotFound，由 service 在内存里兜底到默认等级。
// 这里刻意不提供「查不到就建一行」的方法 —— 那是在访问面读路径写库，
// 不在 AGENTS.md 不变量 1 的两个明文例外里。
func (m *Model) GetAssignment(ctx context.Context, projectID string, userID uint64) (e *AssignmentEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		e, ierr = m.GetAssignmentTx(ctx, tx, projectID, userID)
		return ierr
	})
	return e, err
}

// GetAssignmentTx 在调用方事务内取某访客在某工程的当前归属。
func (m *Model) GetAssignmentTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) (e *AssignmentEntity, err error) {
	var row AssignmentEntity
	if err = tx.WithContext(ctx).Model(&AssignmentEntity{}).
		Where("project_id = ? AND user_id = ?", projectID, userID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// UpsertManualTx 在事务内**原子**写入一条手工指定的归属。
//
// 语义：手工指定总是覆盖当前归属（含覆盖上一条手工指定的），并把 source 置 manual。
// 一条 SQL 承载「读-改-写」——不需要 SELECT FOR UPDATE，因为不存在「先读出来再算」的窗口。
// source 写死在 SET 里而不是取调用方给的值：这是这条路径的定义（手工指定 ⇒ manual），
// 让调用方有机会传错值没有好处。
func (m *Model) UpsertManualTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) error {
	return manualUpsertStmt(ctx, tx, projectID, userID, tierID).Error
}

// manualUpsertStmt 构造「手工指定」的 upsert 语句。
//
// 与执行分开（而不是把整段塞进 Tx 方法）：语句的**形态**是这条路径的全部语义
// （覆盖 + 置 manual），而它只能靠读生成的 SQL 来审 —— 抽成包级函数后，
// model 的单测可以用 gorm 的 DryRun 逐字断言它，不必起数据库。
func manualUpsertStmt(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) *gorm.DB {
	now := time.Now()
	row := &AssignmentEntity{
		ProjectID: projectID, UserID: userID, TierID: tierID,
		Source: SourceManual, AssignedAt: now, CreateTime: now, UpdateTime: now,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"tier_id":     tierID,
			"source":      SourceManual,
			"assigned_at": now,
			"update_time": now,
		}),
	}).Model(&AssignmentEntity{}).Create(row)
}

// UpsertAutoTx 在事务内**原子**写入一条自动重算得出的归属。
//
// 返回 (changed, err)：changed=false 表示**该行被手工锁定，本次自动结论被丢弃**
// （不是错误）。守卫写成 SQL 的 `WHERE membership_assignments.source <> 'manual'`：
//
//	ON CONFLICT (project_id, user_id) DO UPDATE SET ... WHERE membership_assignments.source <> 'manual'
//
// 命中冲突但 WHERE 不成立时 PostgreSQL 影响 0 行且**不报错** —— 正是我们要的
// 「跳过一个被锁定的行，继续处理其余的人」。调用方据 changed 记 Skipped 计数，
// 而不是去猜「为什么这个人的等级没变」。
func (m *Model) UpsertAutoTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) (changed bool, err error) {
	// 语句形态由 autoUpsertStmt 承载（它的 WHERE 就是「手工不被覆盖」这条不变量），
	// 本方法只负责把它执行掉并翻译 RowsAffected。
	res := autoUpsertStmt(ctx, tx, projectID, userID, tierID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// autoUpsertStmt 构造「自动重算」的 upsert 语句 —— 模块最关键的那条 SQL。
//
// 守卫写成 SQL 的 `WHERE membership_assignments.source <> 'manual'`：
// 命中冲突但 WHERE 不成立时 PostgreSQL 影响 0 行且**不报错**，
// 正是我们要的「跳过一个被锁定的行、继续处理其余的人」。
func autoUpsertStmt(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) *gorm.DB {
	now := time.Now()
	row := &AssignmentEntity{
		ProjectID: projectID, UserID: userID, TierID: tierID,
		Source: SourceAuto, AssignedAt: now, CreateTime: now, UpdateTime: now,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"tier_id":     tierID,
			"source":      SourceAuto,
			"assigned_at": now,
			"update_time": now,
		}),
		// DO UPDATE ... WHERE：手工锁定的行在这里被丢掉。
		// 表名限定是必须的（EXCLUDED 之外的行引用在 PG 里要不带别名或带表名）。
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "membership_assignments.source <> ?", Vars: []interface{}{SourceManual}},
		}},
	}).Model(&AssignmentEntity{}).Create(row)
}

// UnlockManualTx 在事务内取消手工锁定（source 改回 auto）。
//
// 只改 source、**不动 tier_id**：解锁本身不该造成一次可见的等级跳变 ——
// 等级要等下一次日结按消费额重算才变，运营因此有机会先看一眼当前值。
// 返回受影响行数：0 表示该归属不是 manual（service 据此返回 ErrManualNotLocked，
// 区分「已经解锁过了」与「点了两次」）。
func (m *Model) UnlockManualTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) (int64, error) {
	res := unlockManualStmt(ctx, tx, projectID, userID)
	return res.RowsAffected, res.Error
}

// unlockManualStmt 构造「取消手工锁定」的更新语句。
//
// 只改 source、**不带 tier_id**：解锁本身不该造成一次可见的等级跳变 ——
// 等级要等下一次日结按消费额重算才变，运营因此有机会先看一眼当前值。
func unlockManualStmt(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) *gorm.DB {
	return tx.WithContext(ctx).Model(&AssignmentEntity{}).
		Where("project_id = ? AND user_id = ? AND source = ?", projectID, userID, SourceManual).
		Updates(map[string]any{"source": SourceAuto, "update_time": time.Now()})
}

// ListAssignmentsTx 在事务内列出某工程的归属（分页 + 筛选，按等级高低与 user_id 排序）。
func (m *Model) ListAssignmentsTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64, page, size int) (list []*AssignmentEntity, err error) {
	err = listAssignmentsStmt(ctx, tx, projectID, tierID, source, userID, page, size).Find(&list).Error
	return list, err
}

// listAssignmentsStmt 构造归属列表的取值语句（分页 + 筛选）。
func listAssignmentsStmt(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64, page, size int) *gorm.DB {
	q := tx.WithContext(ctx).Model(&AssignmentEntity{}).Where("project_id = ?", projectID)
	q = applyAssignmentFilters(q, tierID, source, userID)
	if size > 0 {
		offset := (page - 1) * size
		if offset < 0 {
			offset = 0
		}
		q = q.Offset(offset).Limit(size)
	}
	return q.Order("tier_id DESC, user_id ASC")
}

// CountAssignmentsTx 在事务内统计某工程满足筛选条件的归属数（与 List 共用同一组条件）。
//
// 条件构造只此一处：计数与取值各写一份时，最容易漏的就是筛选维度 ——
// 计数把手工锁定的也算进去，分页条就会凭空多出一页空列表。
func (m *Model) CountAssignmentsTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64) (total int64, err error) {
	err = countAssignmentsStmt(ctx, tx, projectID, tierID, source, userID).Count(&total).Error
	return total, err
}

// countAssignmentsStmt 构造归属计数的语句（与 listAssignmentsStmt 共用筛选条件、不带分页）。
func countAssignmentsStmt(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64) *gorm.DB {
	q := tx.WithContext(ctx).Model(&AssignmentEntity{}).Where("project_id = ?", projectID)
	return applyAssignmentFilters(q, tierID, source, userID)
}

// applyAssignmentFilters 把可选筛选维度叠加到查询上（List / Count 共用）。
func applyAssignmentFilters(q *gorm.DB, tierID int64, source string, userID uint64) *gorm.DB {
	if tierID > 0 {
		q = q.Where("tier_id = ?", tierID)
	}
	if source != "" {
		q = q.Where("source = ?", source)
	}
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	return q
}

// ListAssignments 列出某工程的归属（自带工程作用域事务）。
func (m *Model) ListAssignments(ctx context.Context, projectID string, tierID int64, source string, userID uint64, page, size int) (list []*AssignmentEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		list, ierr = m.ListAssignmentsTx(ctx, tx, projectID, tierID, source, userID, page, size)
		return ierr
	})
	return list, err
}

// CountAssignments 统计某工程的归属数（自带工程作用域事务）。
func (m *Model) CountAssignments(ctx context.Context, projectID string, tierID int64, source string, userID uint64) (total int64, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		total, ierr = m.CountAssignmentsTx(ctx, tx, projectID, tierID, source, userID)
		return ierr
	})
	return total, err
}

// CountByTierTx 统计某等级上挂着的归属数（删除等级前的引用检查）。
//
// 在事务内与删除同事务：先查再删之间若有人新增归属，删除就会留下一条指向已软删等级的归属
// （解析时回落到默认等级，而页面上看不出问题）—— 同事务把这个窗口消掉。
func (m *Model) CountByTierTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (int64, error) {
	var n int64
	err := tx.WithContext(ctx).Model(&AssignmentEntity{}).
		Where("project_id = ? AND tier_id = ?", projectID, tierID).
		Count(&n).Error
	return n, err
}
