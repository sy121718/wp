package migrations

// register_i18n_missing_en_and_unseeded.go — 447 的注册（见 447_i18n_missing_en_and_unseeded.sql 头部）。
//
// 本批两类：
//
//	· 13 个 Msg* 词条（058 落地）只有 zh-CN 行，补 en-US 13 行；
//	· 2 个「模板在用、sys_i18n 与全部迁移里都没有」的 key（admin.media.heading /
//	  admin.article.edit.unavailable），中英各 1 行 —— 共 4 行。
//
// 注册方式：本文件自带 init()（与 442 / 445 / 446 同形），不在 register.go 的 init() 里
// 再加一行 —— 那会让「谁负责注册」出现两个真源。init 的注册顺序不影响执行顺序：
// 种子按 Version 排序（AllSeeds）。
//
// 放在**种子**台账而不是迁移台账：本批只新增词条、不改结构，词条属 seed 语义
// （可重复写入的默认值），与 431 / 442 / 445 / 446 一致。
func init() {
	// ConditionSQL 的语义是**幂等门槛**（migrator.go 的 applySeed：返回 > 0 则跳过），
	// 所以这里判的是「本批的目标行都已经在库里」—— 17 = 15 个 key 的 en-US 行
	// （第一类 13 + 第二类 2）加上第二类 2 个 key 的 zh-CN 行。
	//
	// 为什么按**精确计数 = 17**而不是「>= 1」：门槛一旦在「本批还没跑」时就成立，
	// 整批词条会被静默跳过（431 注释里记的第 416 号同类故障）。第一类的 zh-CN 行
	// 本来就存在，若拿它当门槛就会立刻满足 —— 所以第一类只数 en-US 行。
	// 反过来，缺口被补齐后计数即达 17、每次启动都跳过，不会重复执行；
	// 若某行被误删，条件不成立 → 重跑 seed，而 ON CONFLICT DO NOTHING 只会补缺的行、
	// 不覆盖运营手改过的值（431 / 443 的判据同此）。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换**（applySeed 只 Scan，
	// 不传参），判定用的 key 只能是 SQL 字面量；写成 item_key = ? 会被当成占位符或
	// 缺参而报错（178 / 431 踩过）。
	registerSeed(Seed{
		Version:   "447-i18n-missing-en-and-unseeded",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 17 THEN 1 ELSE 0 END FROM sys_i18n WHERE " +
			"(lang = 'en-US' AND item_key IN (" +
			"'MsgAdminGenericFailed','MsgAdministratorsTitle','MsgBlocksTitle','MsgDatarulesTitle'," +
			"'MsgDepartmentsTitle','MsgMenusTitle','MsgNavigationsTitle','MsgPermissionsTitle'," +
			"'MsgRolesTitle','MsgSiteSettingsSaved','MsgSiteSettingsTitle','MsgThemeSettingsTitle'," +
			"'MsgThemesTitle','admin.media.heading','admin.article.edit.unavailable')) OR " +
			"(lang = 'zh-CN' AND item_key IN ('admin.media.heading','admin.article.edit.unavailable'))",
		SQL: mustSQL("447_i18n_missing_en_and_unseeded.sql"),
	})
}
