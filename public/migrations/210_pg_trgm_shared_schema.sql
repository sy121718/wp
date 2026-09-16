-- 210：把 pg_trgm 固定到专用 schema ext_shared（多 schema / 并发测试的前提）。
--
-- pg_trgm 是**库级唯一**的扩展，而 gin_trgm_ops 按 schema 解析：扩展装在哪个 schema，
-- 只有 search_path 含它的连接才能建 trgm 索引。历史行为是「跟着第一个跑迁移的 schema 走」，
-- 测试的隔离 schema 因此互相踩：
--   · 串行时靠「测试结束 DROP SCHEMA CASCADE 把扩展一并删掉、下个 schema 重新装」侥幸通过；
--   · 并行时后来者的 CREATE EXTENSION IF NOT EXISTS 静默跳过，随后建 trgm 索引直接报
--     operator class "gin_trgm_ops" does not exist，整条迁移失败。
-- 167 / 169 / 173 已改为 WITH SCHEMA ext_shared（新库直接对）；本迁移负责**既有库**：
-- 扩展已经装在某个 schema 里时，把它搬到 ext_shared。
--
-- 选 ext_shared 而不是 public：public 可能承载业务表，让扩展 schema 进入测试 search_path 后
-- 会干扰迁移里 to_regclass 之类的存在性判定（表现为静默跳过整条迁移）。
CREATE SCHEMA IF NOT EXISTS ext_shared;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_extension
        WHERE extname = 'pg_trgm' AND extnamespace <> 'ext_shared'::regnamespace
    ) THEN
        ALTER EXTENSION pg_trgm SET SCHEMA ext_shared;
    END IF;
END $$;
