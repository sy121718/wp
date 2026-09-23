package migrations

import "sync"

// register_client_filter_empty_i18n.go — 客户端筛选「无匹配结果」提示的两条词条（400）。
//
// 背景：menus / departments 两页用 [data-filter-input] 做客户端过滤，此前过滤到 0 行时无任何提示。
// admin.js 的 applyFilter 早就实现了 [data-filter-empty] 的显隐契约，但两页都没有这个元素
// —— 契约存在、消费方缺失。本轮补上元素，文案走 t() 后必须同批 seed，否则英文界面回落中文。
//
// 注册方式：由 register.go 的 init() 显式调用（与 316 / 317 / 399 同形）。
func registerClientFilterEmptyI18n() {
	registerClientFilterEmptyI18nOnce.Do(registerClientFilterEmptyI18nSeed)
}

// registerClientFilterEmptyI18nOnce 让重复调用成为空操作。
var registerClientFilterEmptyI18nOnce sync.Once

// registerClientFilterEmptyI18nSeed 注册 400（真正干活的那一半）。
func registerClientFilterEmptyI18nSeed() {
	// 400：2 个 key × 2 语言。
	//
	// 判定把 key 写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，没有参数替换 ——
	// 写成 item_key = ? 永远查不到行，这条迁移会每次启动重跑（178 踩过）。
	// 门槛 = 两个 key 的 zh-CN 行都在（说明本批已落库，无需再插）。
	registerSeed(Seed{
		Version:   "400-i18n-client-filter-empty",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' " +
			"AND item_key IN ('admin.menus.filter_empty','admin.depts.filter_empty')",
		SQL: mustSQL("400_i18n_client_filter_empty.sql"),
	})
}
