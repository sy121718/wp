-- 763 · 补建 111 的两个时间索引。
--
-- 111 的 CheckSQL 判「表 + 追加写触发器」（历史原文，不改），索引不在判定里：
-- 存量库只要表与触发器在，111 就被判已应用，两个时间索引可能从未创建。
--
-- 列名兼容：111 写的是 created_at，205 之后改名 create_time。按实际列名建索引。
--
-- 幂等：CREATE INDEX IF NOT EXISTS；键与 111 原文一致。

DO $$
DECLARE
    ts_col text;
BEGIN
    SELECT column_name INTO ts_col
      FROM information_schema.columns
     WHERE table_schema = current_schema()
       AND table_name = 'master_data_changes'
       AND column_name IN ('create_time', 'created_at')
     ORDER BY CASE column_name WHEN 'create_time' THEN 0 ELSE 1 END
     LIMIT 1;
    IF ts_col IS NULL THEN
        RAISE EXCEPTION 'master_data_changes 缺少时间列（create_time / created_at）';
    END IF;

    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS idx_master_data_changes_entity ON master_data_changes (project_id, entity_type, entity_id, %I DESC)',
        ts_col);
    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS idx_master_data_changes_time ON master_data_changes (project_id, %I DESC)',
        ts_col);
END $$;
