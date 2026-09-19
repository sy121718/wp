package migrations

// registerIdentityAndMail 注册「访客账号与邮件域（123–134）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerIdentityAndMail() {
	// 123：访客账号模块（issue #36）。七张表一次性建（identity / profile / preferences /
	// roles / sessions / app passwords / meta），CheckSQL 以 users 表存在判定。
	register(Migration{
		Version:   "123-user",
		TableName: "users",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("123_user.sql"),
	})

	// 124：邮箱模块基座（issue #37）。四张表：发信账号 / 模板 / 发送日志 / 抑制名单，
	// CheckSQL 以 mail_accounts 表存在判定。
	register(Migration{
		Version:   "124-mail",
		TableName: "mail_accounts",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("124_mail.sql"),
	})

	// 125：营销域（issue #37）。联系人 / 列表 / 成员 / 活动 / 事件五张表，
	// CheckSQL 以 mail_contacts 表存在判定。
	register(Migration{
		Version:   "125-mail-marketing",
		TableName: "mail_contacts",
		CheckSQL: "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?) THEN 1 ELSE 0 END",
		SQL: mustSQL("125_mail_marketing.sql"),
	})

	// 126：邮箱模块权限点与菜单（issue #37）。
	//
	// 用 Seed 而不是 Migration：Migration 的 CheckSQL 走**参数绑定**（? 是值占位符，表名不能参数化），
	// 只能判定「表或列是否存在」；这里要判定的是「这批数据是否已插入」，Seed 的 ConditionSQL
	// 不带占位符、可写任意条件，正是干这个的。
	registerSeed(Seed{
		Version:      "126-mail-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:account_list'",
		SQL:          mustSQL("126_mail.sql"),
	})

	// 127：邮件日志补活动 / 联系人关联（issue #37），以列存在判定。
	register(Migration{
		Version:   "127-mail-log-links",
		TableName: "mail_logs",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'campaign_id') THEN 1 ELSE 0 END",
		SQL:       mustSQL("127_mail_log_links.sql"),
	})

	// 128：群发活动权限点与菜单（issue #37）。同样用 Seed（见 126 的说明）。
	registerSeed(Seed{
		Version:      "128-mail-campaign",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:campaign_list'",
		SQL:          mustSQL("128_mail_campaign.sql"),
	})

	// 129：内置事务邮件模板（issue #37）。同样用 Seed（见 126 的说明）。
	registerSeed(Seed{
		Version:      "129-mail-templates",
		TableName:    "mail_templates",
		ConditionSQL: "SELECT COUNT(*) FROM mail_templates WHERE template_key = 'register_verify'",
		SQL:          mustSQL("129_mail_templates.sql"),
	})

	// 130：mail_templates.variables 改 text[]（与 tags / target_tags 同一套编解码）。
	register(Migration{
		Version:   "130-mail-template-vars",
		TableName: "mail_templates",
		CheckSQL:  "SELECT CASE WHEN EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'variables' AND data_type = 'ARRAY') THEN 1 ELSE 0 END",
		SQL:       mustSQL("130_mail_template_variables.sql"),
	})

	// 131：自动化流程三张表（issue #38 P3）。
	register(Migration{
		Version:   "131-mail-automation",
		TableName: "mail_automations",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?",
		SQL:       mustSQL("131_mail_automation.sql"),
	})

	// 132：自动化流程权限点与菜单按钮（issue #38 P3）。用 Seed（见 126 说明）。
	registerSeed(Seed{
		Version:      "132-mail-automation",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:automation_list'",
		SQL:          mustSQL("132_mail_automation.sql"),
	})

	// 133：画布位置接口权限点（issue #38 P4）。用 Seed（见 126 说明）。
	registerSeed(Seed{
		Version:      "133-mail-automation-layout",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'mail:automation_layout'",
		SQL:          mustSQL("133_mail_automation_layout_perm.sql"),
	})

	// 134：库存与采购行的商品 / 变体引用完整性。
	register(Migration{
		Version:   "134-inventory-reference-fks",
		TableName: "inventory_stocks",
		// 8 个约束必须**全部**存在才算已执行：只查其中一个的话，部分缺失时会被判成
		// 「已存在」而永久跳过，缺的那几条外键再也不会补上。SQL 本身逐条 IF NOT EXISTS，
		// 判为未执行时重跑是安全的。
		//
		// 必须按 **DISTINCT conname** 计数（2026-09-17 修）：inventory_stock_movements 是
		// **分区表**，每个子分区各带一份同名约束 —— 按行数会膨胀（实测 7 个子分区 ⇒ 同一个
		// fk_inventory_movements_product 出现 7 次，8 个名字实际数出 14 行），于是
		// 「COUNT(*) 等于 8」恒不成立、判定恒 0、**每次启动都重跑 134**。超级用户下 SQL 里的
		// IF NOT EXISTS 把重跑兜住了，所以一直没暴露；换成非超级角色连接（DB-009 换角色）时，
		// 重跑撞 "must be owner of table inventory_stocks (42501)" ⇒ **应用直接启动失败**。
		// 用 DISTINCT 后语义不变（8 个名字必须都出现过），但不再受分区子表重复计数影响。
		CheckSQL: "SELECT CASE WHEN COUNT(DISTINCT c.conname) = 8 THEN 1 ELSE 0 END FROM pg_constraint c WHERE (CAST(? AS text) IS NOT NULL) AND c.conname IN (" +
			"'fk_inventory_stocks_product', 'fk_inventory_purchase_lines_product'," +
			"'fk_inventory_purchase_lines_variant', 'fk_inventory_receipt_items_product'," +
			"'fk_inventory_receipt_items_variant', 'fk_inventory_movements_product'," +
			"'fk_product_price_adjustment_items_product', 'fk_product_price_adjustment_items_variant')",
		SQL: mustSQL("134_inventory_reference_fks.sql"),
	})

	// 276：mail 页面错误归口文案（审计 CQ-009 / 第三波收口，中英各一行）。
	//
	// 词条属于邮箱模块（internal/module/mail/enums/mail_enums.go 的 ErrInternal），
	// 注册在 identity_mail 这一份里（按主题归属）。判定按**本批自己的 key** 计数：
	// 用「全库总量」会被其它批次的行满足而静默跳过（058 踩过，见 226 的注释）；
	// ConditionSQL 里**不能出现 ?** —— 它不接受迁移器传参，带了 ? 会让判定恒为 0、每次启动都重跑。
	registerSeed(Seed{
		Version:      "276-mail-err-internal-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'mail.err.internal'",
		SQL:          mustSQL("276_mail_err_internal_i18n.sql"),
	})
}
