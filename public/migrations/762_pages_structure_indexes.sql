-- 762 · 补建 068 的三个页面索引。
--
-- 068 的 CheckSQL 只判 idx_pages_blockref（历史原文，不改），而它的 SQL 里还有
-- idx_pages_theme_alive / idx_pages_structure_header / idx_pages_structure_footer。
-- 存量库只要已有 blockref，068 就被判已应用，另三个索引从未创建 —— 实测 wp 缺
-- structure_header / structure_footer 两个（blockref 与 theme_alive 在）。
--
-- 为什么另起一条迁移而不是改 068：public/CLAUDE.md「历史迁移 SQL 不改」，
-- 修复走现行执行源（与 447 → 758b 同一手法）。
--
-- 列名兼容：068 写的是 updated_at，205_db019_time_columns_full.sql 之后改名
-- update_time。本迁移按 information_schema 取实际列名，两种状态都能跑。
--
-- 幂等：CREATE INDEX IF NOT EXISTS；表达式与 068 原文逐字一致（PG 按表达式树比较）。

DO $$
DECLARE
    ts_col text;
BEGIN
    SELECT column_name INTO ts_col
      FROM information_schema.columns
     WHERE table_schema = current_schema()
       AND table_name = 'pages'
       AND column_name IN ('update_time', 'updated_at')
     ORDER BY CASE column_name WHEN 'update_time' THEN 0 ELSE 1 END
     LIMIT 1;
    IF ts_col IS NULL THEN
        RAISE EXCEPTION 'pages 缺少时间列（update_time / updated_at）';
    END IF;

    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS idx_pages_theme_alive ON pages (theme_id, %I DESC) WHERE deleted_at IS NULL',
        ts_col);
END $$;

CREATE INDEX IF NOT EXISTS idx_pages_structure_header
    ON pages ((draft_document->'settings'->'structure'->>'headerBlockId'))
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_pages_structure_footer
    ON pages ((draft_document->'settings'->'structure'->>'footerBlockId'))
    WHERE deleted_at IS NULL;
