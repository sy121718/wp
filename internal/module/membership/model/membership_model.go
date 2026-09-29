// Package membershipmodel — membership 模块的表访问单元（Repository，不是 DDD Domain Model）。
//
// 三张表各一个 Entity 与一个句柄，共用一个 Model：
//
//	· membership_tiers        （带 project_id，受 RLS FORCE 策略约束）
//	· membership_entitlements （无 project_id，作用域经 tier_id 传递，不在 RLS 清单里）
//	· membership_assignments  （带 project_id，受 RLS FORCE 策略约束）
//
// 带 project_id 的每一个方法都在 rls.InProjectScope 内执行（迁移 462 同批铺的策略）：
// 依赖策略而不是「调用方记得加 Where」—— 漏加的形态是**静默 0 行**而不是报错，
// 这正是策略存在的理由。按 id 的读写同样要在作用域内：策略是行级的，
// 不设 app.project_id 时 `WHERE id = ?` 也一行都看不见。
package membershipmodel

import (
	"context"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// Model membership 模块的表访问单元。
type Model struct {
	db *gorm.DB
}

// NewModel 构造（db 为整个模块共用的连接）。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 等级表句柄（内部实现细节，只允许被本 model 的仓储方法消费 —— service 不得调用）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&TierEntity{})
}

// EntitlementDB 权益表句柄（同上，仅本 model 内部使用）。
func (m *Model) EntitlementDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&EntitlementEntity{})
}

// AssignmentDB 归属表句柄（同上，仅本 model 内部使用）。
func (m *Model) AssignmentDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AssignmentEntity{})
}

// Transaction 透传事务：跨聚合 / 跨模块编排由 service 决定边界（AGENTS.md §写操作的事务与回滚）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// TransactionScoped 开启事务**并设置工程作用域**，供 service 编排同一工程内多表多步的写。
//
// 为什么不分开暴露：作用域的设置与事务边界是同一件事（set_config(..., is_local => true)
// 只在事务内有效）。让 service 自己「开事务 + 记得调 rls.ScopeTx」的组合，
// 迟早会出现「开了事务忘了设作用域」——那种写法在换非超级角色后每条语句匹配 0 行**且不报错**。
//
// 形态与 page/model/page_tx.go 的 TransactionScoped 一致（同一判据、同一实现）。
func (m *Model) TransactionScoped(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := rls.ScopeTx(tx, projectID); err != nil {
			return err
		}
		return fn(tx)
	})
}
