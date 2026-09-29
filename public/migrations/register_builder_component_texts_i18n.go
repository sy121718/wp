package migrations

// register_builder_component_texts_i18n.go — 构建期组件文案（456）的词条注册。
//
// 见 456_i18n_builder_component_texts.sql 的头部：core.addToCart 与 core.cartIcon
// 两个组件的六处硬编码中文（数量 aria-label / 两条降级提示 / 浮层占位与兜底链接 /
// 缺工程提示）。本批**只新增**这 6 个 key，不复用也不改动既有词条。
//
// 注册方式：本文件自带 init()（与 register_user_order_cart_labels_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func init() {
	// 门槛判据**枚举本批自己的 key**（上界封闭，6 个 key × 2 语言 = 12 行）：
	// 用 LIKE 前缀会让「别批次已有同前缀行」把计数抬高、本批被静默跳过（058 的故障），
	// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的故障）。
	//
	// 挑 zh-CN + en-US 两种语言一起数而不是只数 en-US：本批六句里中文是模板里的
	// 原文，只数中文行会让门槛在「SQL 跑了一半」时也可能凑够（中英成对是本批的硬要求）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** —— 判定用的 key 只能写
	// SQL 字面量；写成 item_key = ? 会被换成表名，判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "456-i18n-builder-component-texts",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'site.component.addToCart.qtyAria'," +
			"'site.component.addToCart.notice.noProject'," +
			"'site.component.addToCart.notice.noVariant'," +
			"'site.component.cartIcon.loading'," +
			"'site.component.cartIcon.viewCart'," +
			"'site.component.cartIcon.notice') AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("456_i18n_builder_component_texts.sql"),
	})
}
