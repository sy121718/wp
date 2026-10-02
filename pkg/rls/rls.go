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
	"database/sql"
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

// ErrNotInTransaction 在**非事务**句柄上设置工程作用域时返回。
//
// 这是个必须显式报错的场景，不能静默放过：set_config(..., is_local => true) 只在
// 当前事务内有效，autocommit 下设完即失效 —— 调用方以为包了 scope，实际策略谓词读到
// 的仍是 NULL，表现为「查询静默返回 0 行」（fail closed 不报错）。与其让对方在
// 「功能突然查不到数据」里排查，不如在入口把误用点出来。
var ErrNotInTransaction = errors.New("rls: 会话变量必须在事务内设置（事务外会立即失效）")

// ScopeTx 在**调用方已开启的事务**上设置工程作用域，不新开事务。
//
// 与 InProjectScope 的分工：InProjectScope 自带事务边界（model 的自足方法用它）；
// 本函数用于「事务已经开着，只是还没设变量」的两个场景：
//   - model 的 *Tx 变体（service 编排事务时传入的 tx），
//   - 已由调用方开启事务的多步读写。
//
// 为什么不能在已开事务上改用 InProjectScope：它的 db 参数若是 model 的裸句柄
// （m.db）而不是外层 tx，就会**另开一个事务、另取一条连接** —— 外层事务里的
// 未提交数据在这个新事务里看不见，锁也可能自撞（同一个 model 内多次调用时），
// 原子性被悄悄破坏。所以要有一个「只设变量、不动事务边界」的入口。
func ScopeTx(tx *gorm.DB, projectID string) error {
	if tx == nil || tx.Statement == nil {
		return ErrNotInTransaction
	}
	// gorm 只在事务里把 ConnPool 换成 *sql.Tx；非事务句柄是 *sql.DB 连接池，
	// 此时设置会随语句自动提交一起失效。类型断言是这里唯一可靠的判据。
	if _, ok := tx.Statement.ConnPool.(*sql.Tx); !ok {
		return ErrNotInTransaction
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return ErrInvalidProjectID
	}
	return tx.Exec("SELECT set_config('"+SettingKey+"', ?, true)", projectID).Error
}

// BypassedRole 报告当前连接的角色是否会**无条件绕过** RLS（superuser / BYPASSRLS）。
//
// 迁移 215 之后策略已在带 project_id 的对象上（数量以 ProbeIdentity 的读数为准，
// 不写死数字：迁移仍在追加），但超级用户总是绕过 RLS —— FORCE 只约束表属主，
// 约束不了 superuser / BYPASSRLS 角色。所以「策略铺好了」不等于「隔离生效」，
// 两者的差别全在这一个布尔值上。
//
// 用途是切换连接角色（DB-009 第二步）时的自检探针：应用连接返回 true 就说明
// RLS 此刻一行都挡不住，即使所有路径都已包 scope。放在 pkg 而不是运维脚本里，
// 是因为它同时是测试断言「这轮隔离验证是不是真的在非超级角色下跑的」的判据。
func BypassedRole(ctx context.Context, db *gorm.DB) (bool, error) {
	// 走 ProbeIdentity 而不是自己再来一条 SQL：这条判据出现在测试夹具、启动探针与运维
	// 脚本三处，各写一条迟早会漂移（例如有人只改了其中一条的 current_user 语义）。
	identity, err := ProbeIdentity(ctx, db)
	if err != nil {
		return false, err
	}
	return identity.BypassesRLS(), nil
}

// InProjectScope 在事务内设置工程作用域后执行 fn。
//
// fn 里拿到的 tx 已经带着隔离上下文：读写都受策略约束（USING 管读、WITH CHECK 管写）。
// 返回 fn 的错误会回滚整个事务。
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
