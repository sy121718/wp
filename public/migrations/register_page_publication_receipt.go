package migrations

// register_page_publication_receipt.go — 激活账本的成功 attempt 身份（迁移 701）。
//
// 判据是「列在且可空 bigint」，而不是「表存在」：page_publications 从建库起就在，
// 只看表名会让这条迁移对既有库永久跳过，而跳过的后果是激活写回执 id 时当场报
// 「列不存在」，且只在真实发布路径上出现。
//
// 三个条件都必要：
//   - data_type = 'bigint'             —— 列型错（如 text）会让 id 比较退化成字符串比较；
//   - is_nullable = 'YES'              —— 历史行与无回执设施的链路必须能写 NULL，
//     误加 NOT NULL 会让这些库的激活全部失败；
//   - column_default IS NULL           —— 本列**不得**有默认值：任何默认值（尤其 0）
//     都会被读成「这是回执 0 的成功尝试」，而 0 不是回执。
//
// CheckSQL 里的 ? 由迁移器传入**表名**（本迁移只有一处占位符，不借用表名判别的技巧）。
//
// 为什么需要表名参数：生产库的 search_path 不一定只有 public（测试库显式是
// "public,ext_shared"，还会带上与库同名的 schema）。写死 'page_publications' 在
// 只有 public 的库上碰巧对，但那是运气；由迁移器传名再配 current_schema() 才是
// 与其余迁移一致的判据 —— 迁移器绑定同一值三次，本迁移只用一次，所以只有一处 ?。
func init() {
	register(Migration{
		Version:   "701-page-publication-receipt",
		TableName: "page_publications",
		CheckSQL: `SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.columns c
			WHERE c.table_schema = current_schema()
			  AND c.table_name = ?
			  AND c.column_name = 'receipt_id'
			  AND c.data_type = 'bigint'
			  AND c.is_nullable = 'YES'
			  AND c.column_default IS NULL`,
		SQL: mustSQL("701_page_publication_receipt.sql"),
	})
}
