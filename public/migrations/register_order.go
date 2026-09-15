package migrations

// registerOrderAndSiteSlots 注册「订单域与页面槽位（135–146）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerOrderAndSiteSlots() {
	// 135：订单头 / 订单项快照 / 状态流转流水（BIZ-1 销售侧）。
	register(Migration{
		Version:   "135-order",
		TableName: "orders",
		// 三张表**全部**建好才算已执行：只查 orders 的话，中途失败会留下
		// 「orders 在、order_items 不在」却被永久跳过的状态（与 134 同因）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name IN ('orders', 'order_items', 'order_status_logs')",
		SQL: mustSQL("135_order.sql"),
	})

	// 136：订单权限点 + 超管策略（8 个权限点全部存在才算已 seed）。
	registerSeed(Seed{
		Version:   "136-order-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:list', 'order:get', 'order:create', 'order:status', " +
			"'order:cancel', 'order:refund', 'order:item_list', 'order:log_list')",
		SQL: mustSQL("136_order_permissions.sql"),
	})

	// 138：系统页面槽位表（BIZ-1：把「结算页是哪一页」这类事实固定下来）。
	// 条件用「表存在」而不是「有行」——空表是合法状态（新工程一个槽位都没绑）。
	register(Migration{
		Version:   "138-page-site-slots",
		TableName: "page_site_slots",
		// CheckSQL 必须接收迁移器传入的表名参数（CAST(? AS text)）——
		// 少了这个占位符，迁移器既无法把它当作「表是否已存在」的检测，
		// 也不会执行建表 SQL（表现是「表不存在」，而不是迁移报错）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name = 'page_site_slots'",
		SQL: mustSQL("138_page_site_slots.sql"),
	})

	// 139：槽位权限点 + 超管策略（3 个权限点全部存在才算已 seed）。
	registerSeed(Seed{
		Version:   "139-page-site-slot-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'page:site_slot_list', 'page:site_slot_bind', 'page:site_slot_unbind')",
		SQL: mustSQL("139_page_site_slot_permissions.sql"),
	})

	// 140：后台菜单入口。
	registerSeed(Seed{
		Version:      "140-page-site-slot-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '系统页面' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("140_page_site_slot_menu.sql"),
	})

	// 137：访客下单自动开号用的「初始密码」邮件模板。
	registerSeed(Seed{
		Version:      "137-guest-account-template",
		TableName:    "mail_templates",
		ConditionSQL: "SELECT COUNT(*) FROM mail_templates WHERE template_key = 'guest_account'",
		SQL:          mustSQL("137_guest_account_template.sql"),
	})

	// 141：优惠码与核销记录（BIZ-1）。两张表都建好才算已执行 ——
	// 只查 coupons 的话，中途失败会留下「券表在、核销表不在」却被永久跳过的状态（与 134/135 同因）。
	register(Migration{
		Version:   "141-order-coupons",
		TableName: "coupons",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name IN ('coupons', 'coupon_redemptions')",
		SQL: mustSQL("141_order_coupons.sql"),
	})

	// 142：优惠码权限点 + 超管策略（7 个权限点全部存在才算已 seed）。
	registerSeed(Seed{
		Version:   "142-order-coupon-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:coupon_list', 'order:coupon_get', 'order:coupon_create', 'order:coupon_update', " +
			"'order:coupon_delete', 'order:coupon_validate', 'order:coupon_redemption')",
		SQL: mustSQL("142_order_coupon_permissions.sql"),
	})

	// 143：订单与优惠码的后台菜单入口（两条都是幂等 seed）。
	registerSeed(Seed{
		Version:      "143-order-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title IN ('订单管理', '优惠码')",
		SQL:          mustSQL("143_order_menu.sql"),
	})

	// 144：退货表（退货单 + 明细）。两张表都建好才算已执行（与 134/135/141 同因）。
	register(Migration{
		Version:   "144-order-returns",
		TableName: "order_returns",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name IN ('order_returns', 'order_return_items')",
		SQL: mustSQL("144_order_returns.sql"),
	})

	// 145：退货权限点 + 超管策略（5 个权限点全部存在才算已 seed）。
	registerSeed(Seed{
		Version:   "145-order-return-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:return_list', 'order:return_get', 'order:return_approve', " +
			"'order:return_reject', 'order:return_receive')",
		SQL: mustSQL("145_order_return_permissions.sql"),
	})

	// 146：订单备注权限点 + 超管策略。
	registerSeed(Seed{
		Version:      "146-order-note-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'order:note'",
		SQL:          mustSQL("146_order_note_permission.sql"),
	})

	// 147：页面浏览记录表（BIZ-8 访问计数）。
	//
	// CheckSQL 必须接收迁移器传入的表名参数（CAST(? AS text)）——缺了这个占位符，
	// 迁移器既无法把它当作「表是否已存在」的检测，也不会执行建表 SQL
	//（表现是「表不存在」，而不是迁移报错）。
	register(Migration{
		Version:   "147-page-views",
		TableName: "page_views",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name = 'page_views'",
		SQL: mustSQL("147_page_views.sql"),
	})
}
