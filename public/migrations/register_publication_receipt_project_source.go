package migrations

// register_publication_receipt_project_source.go — 回执归属新增工程级来源（迁移 703）。
//
// 判据是「publication_receipts_source_type_check 在位，且它的表达式**恰好**是
// source_type IN ('page','presentation','project')」，而不是「表存在」：publication_receipts
// 从 init_builder_schema 起就在，只看表名会让这条迁移对既有库永久跳过，而跳过的后果是
// 工程级下线回执在插入时当场被 CHECK 拒绝 —— 且只在真实删链接的路径上出现。
//
// 为什么不能写成「定义里同时 LIKE 三个值」：那是**包含**判据，不是相等判据。
// 一条被扩宽成四值（例如又加了 'site'）的约束照样命中三条 LIKE，于是这条迁移被判成
// 「已应用」永久跳过 —— 放宽的方向被静默接受，门禁反而看不见。本文件的判据是
// **白名单集合判等**：成员从约束表达式里取，逐字比 array_agg 与应有的三值数组。
// 第四值、少值、改成任意文本、谓词顶层不是「该列 IN (…）」形态，都判 0 → 迁移重跑。
//
// 三条前置条件保证「取成员」这件事本身有意义（缺一条就不判等，直接重跑）：
//   · k.conkey = ARRAY[source_type 的 attnum] —— 被判的就是 source_type 这一列；
//   · source_type 仍是 NOT NULL —— 否则 IN 之外的 NULL 分支重新出现，白名单不再封闭；
//   · 表达式顶层形态是 `source_type = ANY (ARRAY[…]::text[])` —— 所以
//     `(source_type = ANY(…)) OR true` 这类「看着含三个值」的放宽不会被当成相等。
//
// 归一化口径与 507_partition_rls_reconcile.sql 读 pg_get_expr 的思路一致：读 PG 物化出来的
// 表达式，不手拼谓词；IN 的成员在 PG 里落地为 ::text 字面量，逐个取出后按集合比较
// （array_agg … ORDER BY，与书面顺序无关，多加一个值就会变成 4 个元素）。
// 不使用 pg_get_constraintdef 的整串文本做相等比较 —— 那段文本会随 PG 版本变形态，
// 拿它比相等会让同一条迁移在新版本上永远跑；也不拿 pg_indexes.indexdef 做判据。
//
// CheckSQL 里**没有** ? 占位符：迁移器只在 SQL 含 ? 时才绑定表名
//（见 migrator.go 的 ApplyStatements），本迁移的对象名一律写进 SQL 字面量。
//
// 反例（含第四值不得通过、少值不得通过、改成任意文本不得通过）由
// register_publication_receipt_project_source_test.go 在真实迁移库上构造并断言。

func init() {
	register(Migration{
		Version:   "703-publication-receipt-project-source",
		TableName: "publication_receipts",
		CheckSQL:  publicationReceiptSourceTypeCheckSQL,
		SQL:       mustSQL("703_publication_receipt_project_source.sql"),
	})
}

// publicationReceiptSourceTypeCheckSQL 703 的落地判据（零占位符，对象名写死在字面量里）。
//
// 形状是「集合判等」而不是「包含」：regexp_matches 从 pg_get_expr 的表达式里取出 IN 的
// 全部 ::text 字面量，array_agg 排序后与应有的三值数组逐字比较 —— 第四值会让元素数变成 4，
// 少值让元素数变成 2，改成任意文本会让元素对不上，三者都返回 0（→ 迁移重跑）。
const publicationReceiptSourceTypeCheckSQL = `SELECT CASE WHEN (
	EXISTS (
		SELECT 1
		  FROM pg_constraint k
		  JOIN pg_class c ON c.oid = k.conrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'source_type'
		 WHERE n.nspname = current_schema()
		   AND c.relname = 'publication_receipts'
		   AND k.conname = 'publication_receipts_source_type_check'
		   AND k.contype = 'c'
		   AND k.conkey = ARRAY[a.attnum]
		   AND a.attnotnull
		   -- 形态判据作用在**归一化后**的表达式上：PG 物化内联 CHECK 时会保留书写时的外层
		   -- 圆括号（实测 pg_get_expr 给出 "(source_type = ANY (ARRAY['page'::text, …]))"），
		   -- 直接拿原文做前缀匹配会永远拿到 '('。归一化只做两件事、都不改表达式内部：
		   --   · btrim 去首尾空白；
		   --   · regexp_replace 去**一层**首尾圆括号 —— 锚点 ^( 与 )$ 各命中一次，没有 g 标志，
		   --     所以 "(…)" 剥一层、"((…))" 仍是 "(…)"（嵌套属于形态差异 → 判 0 重跑）。
		   --
		   -- 反向引用在 SQL 文本里必须是单个反斜杠 + 1。本例 SQL 写在 Go 原始字符串（反引号）里，
		   -- 源码写一个反斜杠加 1 就得到 SQL 的一个反斜杠加 1（PG 的 ARE 认它为捕获组引用）；
		   -- 写了两个反斜杠时 SQL 收到两个字符，归一化不生效、拿原串去匹配形态，恒判 0（实测踩过）。
		   --
		   -- 形态右端是 ']'，不是 ']::text[]'：内联 CHECK 把类型标在**元素**上
		   -- （ARRAY['page'::text, …]），数组本身没有独立类型标注；实测归一化结果是
		   -- source_type = ANY (ARRAY['page'::text, 'presentation'::text, 'project'::text])。
		   --
		   -- 之后是两端锚定的整串形态匹配（不是包含）：表达式多一段 "OR TRUE" /
		   -- "OR source_type IS NULL" 就落不进这个形状。
		   AND regexp_replace(
				 btrim(pg_get_expr(k.conbin, k.conrelid)),
				 '^\((.*)\)$', '\1') ~
			   '^source_type = ANY \(ARRAY\[.*\]\)$'
		   AND (
			SELECT array_agg(literal ORDER BY literal)
			  FROM (
				SELECT (m.parts)[1] AS literal
				  FROM regexp_matches(
					 pg_get_expr(k.conbin, k.conrelid),
					 '''([^'']*)''::text',
					 'g') AS m(parts)
			  ) s
		   ) = ARRAY['page', 'presentation', 'project']
	)
) THEN 1 ELSE 0 END`
