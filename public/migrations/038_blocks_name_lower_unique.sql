-- 038_blocks_name_lower_unique.sql — blocks 表「同工程块名大小写不敏感」唯一索引。
--
-- 背景：021_blocks.sql 已建 uq_blocks_project_name ON blocks(project_id, name)（大小写敏感）。
-- 而 block_service.Create 用 ExistsByName（LOWER(name)=LOWER(?)）做大小写不敏感判重，
-- 并发下「Foo」与「foo」能穿透精确唯一索引，产生同名（仅大小写不同）脏数据。
-- 本迁移加 LOWER(name) 表达式唯一索引兜底，使判重语义与数据库约束一致。
--
-- 说明：保留原 uq_blocks_project_name 精确索引不动；本 LOWER 索引是更强约束。
-- 脏数据防护：若库中已存在同一 project 下「Foo」与「foo」两条，直接 CREATE 会
-- duplicate key 失败，故先检测（GROUP BY project_id, LOWER(name) HAVING COUNT(*) > 1），
-- 有脏数据则本次跳过建索引（幂等、不报错），待人工清理脏数据后下次启动自动补建。

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM blocks GROUP BY project_id, LOWER(name) HAVING COUNT(*) > 1
  ) THEN
    CREATE UNIQUE INDEX IF NOT EXISTS uq_blocks_project_name_lower
      ON blocks(project_id, LOWER(name));
  END IF;
END $$;
