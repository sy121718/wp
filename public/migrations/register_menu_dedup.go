package migrations

import "sync"

func registerMenuDedupMembership() {
	registerMenuDedupMembershipOnce.Do(registerMenuDedupMembershipSeed)
}

var registerMenuDedupMembershipOnce sync.Once

func registerMenuDedupMembershipSeed() {
	registerSeed(Seed{
		Version:   "634-menu-dedup-membership-customer",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN " +

			"(SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 1 " +
			"AND COALESCE(parent_id, 0) = 0 AND title = '库存') = 1 " +

			"AND (SELECT COUNT(*) FROM sys_menus m " +
			"JOIN sys_menus p ON p.id = m.parent_id AND p.deleted_at IS NULL " +
			"AND p.type = 1 AND p.title = '客户' " +
			"WHERE m.deleted_at IS NULL AND m.type = 2 " +
			"AND m.path IN ('/admin/membership', '/admin/membership/assignments')) = 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("634_menu_dedup_membership.sql"),
	})
}
