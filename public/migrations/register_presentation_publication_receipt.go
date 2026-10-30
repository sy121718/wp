package migrations

// register_presentation_publication_receipt.go — 自动发布实例账本的成功 attempt 身份（迁移 700）。
//
// 判定对象是「形状真的在位」而不是「表存在」：presentation_publications 从建库起就在，
// 只看表名会让这条迁移对既有库永久跳过，而 model 已经开始读写 receipt_id ——
// 跳过表现为运行期 "column does not exist"，且每次发布都失败。
//
// 四个条件都必要（与 701 同形，理由一致）：
//   - data_type = 'bigint'   —— 列型错（如 text）会让 id 比较退化成字符串比较；
//   - is_nullable = 'YES'    —— 历史行与无回执设施的链路必须能写 NULL；
//   - column_default IS NULL —— 本列**不得**有默认值：任何默认值（尤其 0）都会被
//     读成「回执 0 的成功尝试」，而 0 不是回执。
//
// CheckSQL 里的 ? 由迁移器传入**表名**（只有一处占位符，不借用表名判别的技巧）。
func init() {
	register(Migration{
		Version:   "700-presentation-publication-receipt",
		TableName: "presentation_publications",
		CheckSQL: `SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns c
			WHERE c.table_schema = current_schema()
			  AND c.table_name = ?
			  AND c.column_name = 'receipt_id'
			  AND c.data_type = 'bigint'
			  AND c.is_nullable = 'YES'
			  AND c.column_default IS NULL`,
		SQL: mustSQL("700_presentation_publication_receipt.sql"),
	})
}
