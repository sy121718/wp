package migrations

// register_media_variant_full.go — 496：媒体变体槽位改名 webp → full。
//
// 这是**结构迁移**而不是 seed：它改的是既有业务行（sys_media_variant.variant_type）
// 与列注释，判定条件是「还有没有 webp 行」，不需要 ConditionSQL
// （UPDATE 自带幂等性：命中 0 行即已改完）。
//
// 与代码常量同步：mediamodel.VariantTypeFull / image_processor.go 的 buildVariantImage
// / media_reconcile.go 的反查正则都在同一批改动里 —— 任何一侧漏改，
// 表现是「变体记录与磁盘文件对不上」而没有任何报错。
func init() {
	register(Migration{
		Version:   "496-media-variant-full-slot",
		TableName: "sys_media_variant",
		SQL:       mustSQL("496_media_variant_full_slot.sql"),
	})
}
