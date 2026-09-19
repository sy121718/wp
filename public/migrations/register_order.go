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

	// 140：后台菜单入口 —— 已随「系统页面槽位并入主题管理页」下线（迁移 225 删除存量菜单行）。

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

	// 256：成本快照口径收口（2026-09-19 商品域评审）—— 订单行的成本从「变体级
	// product_variants.cost_price」改为「该变体在该行归属仓的当前成本」后，两处结构要跟上：
	//   1. order_items.cost_price 可空（NULL = 下单时该 (仓库, SKU) 尚未核算，
	//      绝不用 0 冒充；0 是合法的显式成本）；
	//   2. inventory_stock_movements.unit_cost（出库时刻的成本留痕，numeric(12,2)）。
	// CheckSQL 判「列已可空且无默认值 / unit_cost 列已存在」，**不是**只判列存在：
	// cost_price 自 135 起就在，只判存在会永远为真、迁移被静默跳过，而「列在但仍是
	// NOT NULL DEFAULT 0」的状态永远修不好（DB-015 的坑，240/244/251 同手法）。
	register(Migration{
		Version:   "256-order-item-cost-nullable-movement-unit-cost",
		TableName: "order_items",
		CheckSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name = 'order_items' AND column_name = 'cost_price' " +
			"AND is_nullable = 'YES' AND column_default IS NULL" +
			") = 1 AND (" +
			"SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() " +
			"AND table_name = 'inventory_stock_movements' AND column_name = 'unit_cost'" +
			") = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("256_order_item_cost_nullable.sql"),
	})

	// 270：入库入口的 SKU 编码校验词条 ErrStockSKURequired（中英各一行）。
	//
	// 词条本身属于**库存域**（internal/module/product/inventory/enums），但本批的注册文件
	// 授权只到 register_order.go（其余 register_*.go 属别的批次），所以按批次约定登记在这里。
	// 判定按本批自己的 key 计数：用「全库总量」会被其它批次的行满足而静默跳过
	//（058 踩过，见 226 的注释）；ConditionSQL 里**不能出现 ?** —— 它不接受迁移器传参，
	// 带了 ? 会让判定恒为 0、每次启动都重跑。
	registerSeed(Seed{
		Version:      "270-inventory-stock-sku-required-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'ErrStockSKURequired'",
		SQL:          mustSQL("270_inventory_stock_sku_required_i18n.sql"),
	})

	// 272：采购入库页补回「生产入库」表单时新增的 11 个文案位（中英成对，22 行）。
	//
	// 路由 /admin/inventory/purchases/production、权限点 inventory:purchase_production 与
	// handler 一直在，缺的只是页面入口 —— 本批把表单补回采购入库页
	//（internal/templates/admin/inventory_purchases.html）。模板文案一律走 t(key, 中文兜底)，
	// 词条必须同批 seed 中英各一行：缺 en-US 不会有任何断言变红，只会让英文界面显示中文。
	//
	// 判定按**本批自己的 key 列表**计数（用「全库总量」会被其它批次的行满足而静默跳过，
	// 058 踩过；用 LIKE 前缀也会被将来新增的同前缀 key 带跑）；ConditionSQL 里不能出现 ? ——
	// 它不接受迁移器传参，带了 ? 会让判定恒为 0、每次启动都重跑。
	registerSeed(Seed{
		Version:   "272-inventory-production-inbound-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 22 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.inventory_purchases.production.title', 'admin.inventory_purchases.production.open', " +
			"'admin.inventory_purchases.production.noInternal', 'admin.inventory_purchases.production.labelSource', " +
			"'admin.inventory_purchases.production.optionSource', 'admin.inventory_purchases.production.labelSku', " +
			"'admin.inventory_purchases.production.optionSku', 'admin.inventory_purchases.production.labelQuantity', " +
			"'admin.inventory_purchases.production.labelCost', 'admin.inventory_purchases.production.hintCost', " +
			"'admin.inventory_purchases.production.submit')",
		SQL: mustSQL("272_inventory_production_inbound_i18n.sql"),
	})
}
