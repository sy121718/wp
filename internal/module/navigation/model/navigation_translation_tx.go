package navigationmodel

// navigation_translation_tx.go — 导航译文写路径需要的**事务内**读法。
//
// 为什么不能直接用既有的 List：List 走 rls.InProjectScope，而 InProjectScope 会
// **新开一个事务、另取一个连接**（见 pkg/rls）。service 手里那个还开着的事务因此读不到
// 它自己刚写的东西，也读不到「锁在同一连接上」的那份快照 —— 两者根本不是同一个会话。
// 写路径要求「读在调用方事务内、同一连接、同一快照」，所以这里补一个 Tx 变体。
//
// 只读、不加业务规则：SQL 与 List 逐字相同（同一列集、同一 ORDER BY），差别只有连接与
// 作用域：作用域在**传入的 tx** 上用 rls.ScopeTx 设置（set_config 只在事务内有效，
// 所以既不能在事务外设、也不能指望调用方替我们设 —— AGENTS.md 不变量：作用域与事务边界
// 是同一件事）。
//
// 层归属：model —— 只碰本模块的表（navigations），不跨聚合、不含业务规则、不拼消息。

import (
	"context"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ListTx 在外部事务内按工程（可选 kind）列出导航项，sort_order 升序、同序按 id 升序。
//
// 与 List 的唯一差别是连接与作用域：这里在 tx 上设作用域，因此调用方事务内的后续写
// 看到的是同一份快照，也不会出现「锁在一根连接、读在另一根连接」的分叉。
func (m *Model) ListTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (list []*NavigationEntity, err error) {
	if tx == nil {
		// 没有事务句柄就不存在「事务内读」：显式失败，不偷偷退回 m.db（那会得到另一份快照，
		// 调用方还以为自己读的是锁内的数据）。
		return nil, ErrTransactionRequired
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	q := tx.WithContext(ctx).Model(&NavigationEntity{}).Where("project_id = ?", projectID)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if err := q.Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// Transaction 开启事务并执行 fn：fn 返回 error 即整体回滚。
//
// 与 page / presentation 的同名方法同形（m.db.WithContext(ctx).Transaction(fn)）：
// 「事务边界在 service 决定、句柄从 model 下传」是本仓既有形状，这里只是补上 navigation
// 缺的那一条（此前本模块所有写都是单语句自动提交）。
//
// 作用域**不在这里设**：译文写的顺序是「开事务 → 设作用域 → 取内容写锁」，
// 而作用域需要规范工程 id（由 service 归一），model 不该猜；调用方按自己的步骤设。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if m == nil || m.db == nil {
		return ErrTransactionRequired
	}
	return m.db.WithContext(ctx).Transaction(fn)
}
