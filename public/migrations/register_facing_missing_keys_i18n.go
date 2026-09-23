package migrations

import "sync"

// register_facing_missing_keys_i18n.go — 四个「已进白名单、却从未登记词条」的 key（429）。
//
// 见 429_i18n_facing_missing_keys.sql 的头部。这批 key 的病不是「少了一句提示」，
// 而是白名单与 sys_i18n 是**两处独立登记**、漏了后一处不会有任何报错：
// 取词函数只能按 fallback 回落，而这几个位置的 fallback 恰好就是 key 本身，
// 于是把内部常量名（`ErrInvalidSlot`）摆到了运营面前。
//
// 共 4 个 key × 2 语言 = 8 行：
//
//	ErrInvalidRange  —— analytics 白名单（analyticsFacingMessages）命中后经 shell.TranslateFor 取词
//	ErrInvalidParent —— navigation 白名单（NavigationFacingMessages）命中后经 localizeFacing 取词
//	ErrInvalidSlot   —— page 槽位页白名单（siteSlotFacingMessages）的键
//	ErrSlotPageMiss  —— 同上（要绑定的页面不存在 / 不属于当前工程）
//
// 注册方式：本文件自带 init()（与 register_page_translations_component_i18n.go 同形）。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerFacingMissingKeysI18n() {
	registerFacingMissingKeysI18nOnce.Do(registerFacingMissingKeysI18nSeed)
}

// registerFacingMissingKeysI18nOnce 让重复调用成为空操作。
var registerFacingMissingKeysI18nOnce sync.Once

// registerFacingMissingKeysI18nSeed 注册 429（真正干活的那一半）。
func registerFacingMissingKeysI18nSeed() {
	// 门槛 = 本批 4 个 key 的 **en-US** 行都在（= 4 行）。挑 en-US 而不是 zh-CN 是有意的：
	// 中文行与 Go 侧的 fallback 同形，容易在别处被顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立，整批词条被静默跳过。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行，**没有任何参数替换** —— 判定要用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会永远查不到行（? 被换成表名），让这条迁移每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "429-i18n-facing-missing-keys",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'ErrInvalidRange','ErrInvalidParent','ErrInvalidSlot','ErrSlotPageMiss')",
		SQL: mustSQL("429_i18n_facing_missing_keys.sql"),
	})
}

func init() { registerFacingMissingKeysI18n() }
