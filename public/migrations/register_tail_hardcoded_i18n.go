package migrations

import "sync"

// register_tail_hardcoded_i18n.go — 后台模板硬编码中文清零批次的词条（435）。
//
// 见 435_i18n_tail_hardcoded.sql 的头部：三个模板的 19 行硬编码中文（locale_rows.html 语言行
// 片段 / theme_settings.html 折叠卡注记 / product_attribute_rows.html 属性值编辑行），
// 本批新增 14 个 key（每个两行 zh-CN + en-US），另有 4 个既有词条被复用而不重复插入
// —— 复用项不进门槛计数，判定只看本批新增的 14 个。
//
// 注册方式：本文件自带 init()（与 register_customers_status_help_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerTailHardcodedI18n() {
	registerTailHardcodedI18nOnce.Do(registerTailHardcodedI18nSeed)
}

// registerTailHardcodedI18nOnce 让重复调用成为空操作。
var registerTailHardcodedI18nOnce sync.Once

// registerTailHardcodedI18nSeed 注册 435（真正干活的那一半）。
func registerTailHardcodedI18nSeed() {
	// 门槛 = 本批 14 个新 key 的 **en-US** 行都在（= 14 行）。挑 en-US 而不是 zh-CN：
	// 中文行与模板里的兜底同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立、整批词条被静默跳过（416 / 431 的同一理由）。
	//
	// 放在**种子**台账：本批只新增词条、不改任何既有 key，词条属于 seed 语义
	//（可重复写入的默认值），与 431 / 190 / 232 等 seed 无先后约束。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** —— 判定用的 key 只能写
	// SQL 字面量；写成 item_key = ? 会被换成表名，判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "435-i18n-tail-hardcoded",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 14 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.common.locale.ph.lang','admin.common.locale.enabled','admin.common.locale.empty'," +
			"'admin.theme_settings.group_items','admin.theme_settings.global_blocks_note'," +
			"'admin.theme_settings.structure_templates_note'," +
			"'admin.product_attributes.values.empty','admin.product_attributes.values.col.label'," +
			"'admin.product_attributes.values.col.sort','admin.product_attributes.values.col.status'," +
			"'admin.product_attributes.values.ph.key','admin.product_attributes.values.ph.label'," +
			"'admin.product_attributes.values.enabled','admin.product_attributes.values.add')",
		SQL: mustSQL("435_i18n_tail_hardcoded.sql"),
	})
}

func init() { registerTailHardcodedI18n() }
