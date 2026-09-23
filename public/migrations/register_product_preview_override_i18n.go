package migrations

import "sync"

// register_product_preview_override_i18n.go — 操作列「预览」文案的覆盖迁移（316）。
//
// 为什么要一条**覆盖**迁移而不是再插一次 seed：admin.products.row.preview 在 191 就已
// seed 成「预览详情页」，而 seed 一律 INSERT ... ON CONFLICT DO NOTHING —— 键已存在即跳过，
// 314 里再写一遍是无效的（页面继续显示旧值，模板里的 fallback「预览」根本不参与）。
// 与 254 同一手法：UPDATE 旧值，历史迁移保持原样。
//
// 注册方式：由 register.go 的 init() 显式调用（与 294 / 298 / 304 / 314 / 315 同形）。
func registerProductPreviewOverrideI18n() {
	registerProductPreviewOverrideI18nOnce.Do(registerProductPreviewOverrideI18nSeed)
}

// registerProductPreviewOverrideI18nOnce 让重复调用成为空操作。
var registerProductPreviewOverrideI18nOnce sync.Once

// registerProductPreviewOverrideI18nSeed 注册 316（真正干活的那一半）。
func registerProductPreviewOverrideI18nSeed() {
	// 316：1 个 key × 2 语言。
	//
	// 幂等判定把 key 与目标译文写进 **SQL 字面量**：ConditionSQL 由迁移器直接 db.Raw 执行，
	// 没有任何参数替换 —— 写成 item_key = ? 永远查不到行，这条迁移会每次启动重跑（178 踩过）。
	// 门槛 = 「两种语言的当前值都已是目标文案」，此时无需 UPDATE；任一语言还是旧值就执行。
	registerSeed(Seed{
		Version:   "316-i18n-override-product-preview",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.products.row.preview' " +
			"AND ((lang = 'zh-CN' AND item_value = '预览') OR (lang = 'en-US' AND item_value = 'Preview'))",
		SQL: mustSQL("316_i18n_override_product_preview.sql"),
	})
}
