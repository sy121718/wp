package migrations

func init() {
	register(Migration{
		Version:   "703-publication-receipt-project-source",
		TableName: "publication_receipts",
		CheckSQL:  publicationReceiptSourceTypeCheckSQL,
		SQL:       mustSQL("703_publication_receipt_project_source.sql"),
	})
}

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
