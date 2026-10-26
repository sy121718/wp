package migrations

import "sync"

// register_menu_dedup.go — 后台菜单去重（库存目录两套）+ 会员两项归位「客户」（634）。
//
// **seed 形态而不是 Migration**：Migration 的默认检查只看 sys_menus 表在不在（早就在），
// 自定义 CheckSQL 还得按表名传参；本批判的是**数据结果** —— 库存目录是否唯一、会员两项
// 是否已挂在「客户」目录下，正是 ConditionSQL 的形状。
//
// 判据的偏差方向是「宁可重跑」：目录数不等于 1、或两项会员不在客户目录下 → 返回 0 重跑。
// SQL 全是「值相同则 0 行改动」的 UPDATE，多跑一次不会产生第二种结果。
// 反过来（判据只看目录存在）会让「目录唯一但会员还在根级」的中间态被永久跳过。
func registerMenuDedupMembership() {
	registerMenuDedupMembershipOnce.Do(registerMenuDedupMembershipSeed)
}

// registerMenuDedupMembershipOnce 让重复调用成为空操作（register.go 只调一次，
// init 另外注册的条目不得再调 —— 重复 registerSeed 会撞版本号、ValidateRegistry 直接报错）。
var registerMenuDedupMembershipOnce sync.Once

// registerMenuDedupMembershipSeed 注册 634（真正干活的那一半）。
func registerMenuDedupMembershipSeed() {
	registerSeed(Seed{
		Version:   "634-menu-dedup-membership-customer",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +
			// 库存目录唯一
			"(SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 1 " +
			"AND COALESCE(parent_id, 0) = 0 AND title = '库存') = 1 " +
			// 两项会员都挂在「客户」目录下（按 parent 连接判，不看 title：title 可被运营改）
			"AND (SELECT COUNT(*) FROM sys_menus m " +
			"JOIN sys_menus p ON p.id = m.parent_id AND p.deleted_at IS NULL " +
			"AND p.type = 1 AND p.title = '客户' " +
			"WHERE m.deleted_at IS NULL AND m.type = 2 " +
			"AND m.path IN ('/admin/membership', '/admin/membership/assignments')) = 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("634_menu_dedup_membership.sql"),
	})
}
