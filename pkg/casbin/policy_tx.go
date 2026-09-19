package casbin

// policy_tx.go — 策略写的「同事务」变体（2026-09-19 全模块事务审计：admin 权限点 / 角色三处）。
//
// 背景：一次可感知的策略变更同时改两处持久化状态 ——
//   · 改权限点的 api_path / api_method：sys_permission 行 + sys_casbin_rule 里该权限点的全部 p 行；
//   · 启停角色：sys_role.status + sys_casbin_rule 的 g2 行。
// 旧实现两处各自提交：策略删完、Add 中途失败就留下「旧策略已删、新策略没写完」的残缺态
// （该权限点连带超管全员 403），禁用角色失败则相反 —— 库里已禁用、g2 还在，角色仍被放行。
//
// 为什么不让 enforcer / adapter 接进调用方事务：
//   · 自研 Adapter 持有的是**连接池句柄**（Adapter.db），casbin 的 AddPolicy / RemovePolicy
//     在 Enforcer 自己的锁内另取连接执行，没有接收外部 *gorm.DB 的入口；
//   · Enforcer.SetAdapter 换句柄不可行：LoadPolicy 也走同一个句柄，而事务回滚这件事
//     没人能通知 Enforcer —— 副本会与库里已回滚的数据漂移；
//   · casbin v3.10 的 TransactionalEnforcer + persist.TransactionalAdapter 同样不可行：
//     BeginTransaction(ctx) 只收 ctx（事务由 adapter 自己开，与调用方事务是两个事务），
//     而且它包的是 *Enforcer 而非本项目使用的 *SyncedEnforcer（审计 C1/H1/H2 的并发保证）。
//
// 采用的形状：**sys_casbin_rule 是 pkg/casbin 自己拥有的表**，策略行直接用调用方的
// *gorm.DB 做参数化 SQL —— 与业务行落在**同一个** PostgreSQL 事务里，提交/回滚由数据库保证。
// Enforcer 的内存副本（策略树 + urlCodeMap）是**派生数据**（启动时由 LoadPolicy 从这张表重建）：
// 事务成功后由调用方调 ReloadPolicy 从库重建；事务回滚时不动副本，副本与库始终一致。
//
// 调用约定：
//   · …Tx 系列必须传入一个**已开启的事务句柄**（service 在 model.Transaction 闭包内调用）；
//   · 事务成功提交后必须调 ReloadPolicy（否则内存副本仍是旧策略，表现为「改了没生效」；
//     下一次任何策略写或进程重启都会自动收敛到库里的真相）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 策略类型常量：与 casbin 模型定义（casbin.go 的 rbacModel）保持一致。
const (
	policyTypePermission = "p"  // p, sub, obj, act, code
	policyTypeGrouping   = "g"  // g, user_id, role_code
	policyTypeRoleActive = "g2" // g2, role_code, active
)

// roleActiveMarker g2 行的固定第二字段（见 rbacModel 的 g2(p.sub, "active")）。
const roleActiveMarker = "active"

// ReplacePermissionDefinitionTx 在调用方事务内替换权限点的请求路径与方法。
//
// 语义与旧版 ReplacePermissionDefinition 一致：保留该权限点上的授权主体（v0 = 角色编码或 user_id），
// 只把 obj/act 换成新定义；该权限点没有任何授权时是空操作。
//
// 返回值 changed 表示是否真的改动了策略行（false 时调用方无需重载内存副本）。
//
// 顺序说明：这里用「先删后加」，与旧的非事务实现一致 —— 它能把 v4/v5 为 NULL 的历史行
// 一并清掉（迁移里的 INSERT 只写 ptype..v3），最终状态恰好是「每个主体一行、v4/v5 为空串」。
// 旧实现依赖的「先加后删」是为了在没有事务时缩短残缺窗口；有了事务，原子性由数据库提供，
// 顺序不再承担安全职责，行级锁覆盖的键集合也与先加后删完全相同。
func ReplacePermissionDefinitionTx(ctx context.Context, tx *gorm.DB, code, path, method string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if code == "" || path == "" || method == "" {
		return false, errors.New("权限编码、请求路径和方法不能为空")
	}
	method = strings.ToUpper(method)

	// 读出该权限点当前的授权主体（与旧实现取 GetFilteredPolicy(3, code) 等价）。
	var existing []CasbinRule
	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v3 = ?", policyTypePermission, code).
		Order("id ASC").Find(&existing).Error; err != nil {
		return false, fmt.Errorf("查询权限策略失败: %w", err)
	}

	subjects := make([]string, 0, len(existing))
	seen := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		if row.V0 == "" {
			continue
		}
		if _, ok := seen[row.V0]; ok {
			continue
		}
		seen[row.V0] = struct{}{}
		subjects = append(subjects, row.V0)
	}
	if len(subjects) == 0 {
		return false, nil
	}

	// 删掉该权限点的全部旧 p 行（含 v4/v5 为 NULL 的历史行），再按新定义写回。
	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v3 = ?", policyTypePermission, code).
		Delete(&CasbinRule{}).Error; err != nil {
		return false, fmt.Errorf("删除权限旧策略失败: %w", err)
	}
	for _, sub := range subjects {
		line := CasbinRule{Ptype: policyTypePermission, V0: sub, V1: path, V2: method, V3: code}
		if err = tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error; err != nil {
			return false, fmt.Errorf("写入权限新策略失败（%s %s）：%w", method, path, err)
		}
	}
	return true, nil
}

// ActivateRoleTx 在调用方事务内写入 g2 启用标记（幂等）。
// 返回值 changed 表示此前不存在该标记、本次真的插入了。
func ActivateRoleTx(ctx context.Context, tx *gorm.DB, roleCode string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	line := CasbinRule{Ptype: policyTypeRoleActive, V0: roleCode, V1: roleActiveMarker}
	result := tx.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).Create(&line)
	if result.Error != nil {
		return false, fmt.Errorf("启用角色失败: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

// DeactivateRoleTx 在调用方事务内删除 g2 启用标记（幂等）。
// 不删 p / g 关联，重新启用调 ActivateRoleTx 即可。
// 返回值 changed 表示此前存在该标记、本次真的删除了。
func DeactivateRoleTx(ctx context.Context, tx *gorm.DB, roleCode string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	result := tx.WithContext(ctx).
		Where("ptype = ? AND v0 = ? AND v1 = ?", policyTypeRoleActive, roleCode, roleActiveMarker).
		Delete(&CasbinRule{})
	if result.Error != nil {
		return false, fmt.Errorf("禁用角色失败: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

// DeleteRoleAllPoliciesTx 在调用方事务内删除角色关联的全部策略（p / g / g2）。
//
// 语义与 DeleteRoleAllPolicies 完全一致：
//   - 该角色的全部 p 行（角色拥有的权限，v0 = role_code）；
//   - 所有用户与该角色的 g 行（用户角色绑定，按 obj 过滤，v1 = role_code）；
//   - 该角色的 g2 启用标记（v0 = role_code, v1 = active）。
//
// 返回值 changed 表示是否真的删掉了行（false 时调用方无需重载内存副本）。
//
// 为什么必须有事务版：删角色是「策略行 + sys_role 行」两处持久化写。旧实现先删策略、
// 后删实体，中途失败就留下「角色还在、权限一个不剩」—— 该角色下所有人瞬间失权，
// 而报错只回给这一次请求，运维从列表页看不出异常。与 PermUpdate / RoleUpdate 同一形态。
func DeleteRoleAllPoliciesTx(ctx context.Context, tx *gorm.DB, roleCode string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	// 三个删除范围与旧实现逐条对应（顺序无关，都在同一个事务里）。
	scopes := []struct {
		cond string
		args []any
	}{
		{"ptype = ? AND v0 = ?", []any{policyTypePermission, roleCode}},
		{"ptype = ? AND v1 = ?", []any{policyTypeGrouping, roleCode}},
		{"ptype = ? AND v0 = ? AND v1 = ?", []any{policyTypeRoleActive, roleCode, roleActiveMarker}},
	}

	var total int64
	for _, scope := range scopes {
		res := tx.WithContext(ctx).Where(scope.cond, scope.args...).Delete(&CasbinRule{})
		if res.Error != nil {
			return false, fmt.Errorf("删除角色策略失败: %w", res.Error)
		}
		total += res.RowsAffected
	}
	return total > 0, nil
}

// DeleteUserAllPoliciesTx 在调用方事务内删除用户的直接权限与角色绑定（p / g）。
//
// 语义与 DeleteUserAllPolicies 完全一致：
//   - p 行中该用户的直接额外权限（v0 = user_id）；
//   - g 行中该用户与角色的绑定（v0 = user_id）。
//
// 返回值 changed 表示是否真的删掉了行（false 时调用方无需重载内存副本）。
//
// 为什么必须有事务版：删管理员是「sys_admin 行 + 该账号的全部策略行」两处持久化写。
// 旧实现先删实体、后逐条删策略，中途失败就留下「管理员没了、策略还在」—— 更糟的是
// 若反过来先删策略再删实体失败，则账号还在却已失权；两种半截状态都不可接受。
func DeleteUserAllPoliciesTx(ctx context.Context, tx *gorm.DB, userID string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if userID == "" {
		return false, errors.New("用户 ID 不能为空")
	}

	scopes := []struct {
		cond string
		args []any
	}{
		{"ptype = ? AND v0 = ?", []any{policyTypePermission, userID}},
		{"ptype = ? AND v0 = ?", []any{policyTypeGrouping, userID}},
	}

	var total int64
	for _, scope := range scopes {
		res := tx.WithContext(ctx).Where(scope.cond, scope.args...).Delete(&CasbinRule{})
		if res.Error != nil {
			return false, fmt.Errorf("删除用户策略失败: %w", res.Error)
		}
		total += res.RowsAffected
	}
	return total > 0, nil
}

// ReplacePermissionDefinition 保留授权主体并替换权限的请求路径和方法。
//
// 非事务版本：自行开一个事务包住「策略行替换」，提交成功后重载内存副本。
// service 层若还有**同一事务内**的业务行要写，必须改用 ReplacePermissionDefinitionTx
// 把策略写并进那个事务（否则就是本文开头说的那个残缺态）。
func ReplacePermissionDefinition(code, path, method string) error {
	return runPolicyTx(func(ctx context.Context, tx *gorm.DB) (bool, error) {
		return ReplacePermissionDefinitionTx(ctx, tx, code, path, method)
	})
}

// ActivateRole 启用角色：写入 g2, roleCode, active（幂等）。
func ActivateRole(roleCode string) error {
	return runPolicyTx(func(ctx context.Context, tx *gorm.DB) (bool, error) {
		return ActivateRoleTx(ctx, tx, roleCode)
	})
}

// DeactivateRole 禁用角色：删除 g2, roleCode, active（幂等）。
func DeactivateRole(roleCode string) error {
	return runPolicyTx(func(ctx context.Context, tx *gorm.DB) (bool, error) {
		return DeactivateRoleTx(ctx, tx, roleCode)
	})
}

// runPolicyTx 非事务变体共用：在连接池上开一个事务执行策略写，提交成功后重载内存副本。
//
// 这里**不**持有 policyMu：策略写走的是数据库行锁，而 Enforcer 路径的写（ReplaceRolePermissions 等）
// 持有 policyMu 且可能阻塞在我们已锁住的策略行上 —— 再让本函数等 policyMu 就会互等。
// 内存副本的刷新（ReloadPolicy）才需要 policyMu，它在事务提交之后单独获取。
func runPolicyTx(fn func(ctx context.Context, tx *gorm.DB) (bool, error)) error {
	db, err := policyDB()
	if err != nil {
		return err
	}

	changed := false
	if err := db.Transaction(func(tx *gorm.DB) error {
		c, err := fn(context.Background(), tx)
		if err != nil {
			return err
		}
		changed = c
		return nil
	}); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return ReloadPolicy()
}

// policyDB 返回策略表所在的连接池句柄 —— 即 Enforcer 的 adapter 持有的那一个，
// 保证事务开在与策略表、enforcer 完全相同的连接池上（不引入第二个数据源）。
func policyDB() (*gorm.DB, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	a, ok := e.GetAdapter().(*Adapter)
	if !ok || a == nil {
		return nil, fmt.Errorf("casbin 适配器类型不支持事务：%T", e.GetAdapter())
	}
	if a.db == nil {
		return nil, fmt.Errorf("casbin 适配器未绑定数据库句柄")
	}
	return a.db, nil
}
