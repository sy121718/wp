package migrations

import "sync"

// register_page_translations_component_i18n.go — 翻译工作台合并成一张编辑表后的
// 「组件」列头词条（428）。
//
// 见 428_i18n_page_translations_component_col.sql 的头部：admin/page_translations.html
// 从「每个组件一张卡 + 一张表」改成「一张表按组件分段」，表头多出第一列「组件」
// （组件名由表内合并整行的分组行承担，原先是每组一个 <h2>）。
//
// 共 1 个 key × 2 语言：
//
//	admin.page_translations.col.component —— 六列表头的首列（组件）
//
// 注册方式：本文件自带 init()（与 register_page_page_err_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序，
// RunSeeds 在所有结构迁移之后执行。
func registerPageTranslationsComponentI18n() {
	registerPageTranslationsComponentI18nOnce.Do(registerPageTranslationsComponentI18nSeed)
}

// registerPageTranslationsComponentI18nOnce 让重复调用成为空操作。
var registerPageTranslationsComponentI18nOnce sync.Once

// registerPageTranslationsComponentI18nSeed 注册 428（真正干活的那一半）。
func registerPageTranslationsComponentI18nSeed() {
	// 门槛 = 本批 1 个 key 的 **en-US** 行在（= 1 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行（? 被换成表名），让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "428-i18n-page-translations-component-col",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.page_translations.col.component')",
		SQL: mustSQL("428_i18n_page_translations_component_col.sql"),
	})
}

func init() { registerPageTranslationsComponentI18n() }
