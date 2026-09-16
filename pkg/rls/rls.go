// Package rls 工程隔离的行级安全（RLS）原语（审计 DB-009）。
//
// 为什么需要它：带 project_id 的表在数据库层启用了 ROW LEVEL SECURITY +
// FORCE ROW LEVEL SECURITY（迁移 199 试点、215 铺开），策略谓词读会话变量
// app.project_id。变量必须在**事务内**用 set_config(..., is_local => true) 设置 ——
// 事务结束自动还原，GORM 连接池复用连接时不会把本工程的隔离上下文泄漏给下一个请求。
//
// 会话级（is_local => false）设置在这里是错的：连接归还池中后变量仍然在，
// 下一个拿到该连接的请求若不设变量就会**继承上一个工程的 id** ——
// 那比不做隔离更糟（fail open 到别的工程，而不是查不到数据）。
//
// 本包只提供原语，不持有任何 model：各模块的 model 自己把 db 传进来。
// 这是 DB-009 想消除的重复 —— 此前 withProjectScope 只在 project/model 内部，
// 别的模块要用只能复制一份。
package rls

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SettingKey 会话变量名。策略谓词（迁移 199/215）里的字面量必须与它逐字一致。
const SettingKey = "app.project_id"

// ScopedPredicate 单工程隔离的策略谓词 —— 与迁移 199 / 215 里的字面量**逐字一致**。
//
// 导出的理由：除了建策略的迁移，还有一处运行时也要用同一份谓词 ——
// partition.EnsureAhead 给新建的月分区补隔离策略（PG 的 ENABLE/FORCE 不递归到分区）。
// 两处各写一遍的话，改动时漏掉一处就会出现「父表与子表谓词不同」这种极难发现的偏差。
const ScopedPredicate = "(project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)"

// GlobalPredicate 放行 project_id IS NULL 的**全局行**（内置变动原因、全局词条）。
//
// 与 ScopedPredicate 一样，WITH CHECK 也用同一谓词 —— 否则 seed 写内置原因会被
// FORCE 挡死（seed 走的是迁移连接，属主同样受 WITH CHECK 约束）。
const GlobalPredicate = "(project_id IS NULL OR project_id = " +
	"NULLIF(current_setting('app.project_id', true), '')::uuid)"

// ErrInvalidProjectID 工程 id 不是合法 uuid。
//
// 先校验再进 SQL：策略谓词里有 ::uuid 强转，非法值会把 PG 的
// "invalid input syntax for type uuid" 直接抛给调用方，错误归属变得难判断
// （看起来像数据库故障，实际是入参错）。
var ErrInvalidProjectID = errors.New("rls: 工程 id 不是合法 uuid")

// InProjectScope 在事务内设置工程作用域后执行 fn。
//
// fn 里拿到的 tx 已经带着隔离上下文：读写都受策略约束（USING 管读、WITH CHECK 管写）。
// 返回 fn 的错误会回滚整个事务。
func InProjectScope(ctx context.Context, db *gorm.DB, projectID string, fn func(tx *gorm.DB) error) error {
	if _, err := uuid.Parse(projectID); err != nil {
		return ErrInvalidProjectID
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('"+SettingKey+"', ?, true)", projectID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
