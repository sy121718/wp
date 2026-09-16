-- 223 · content_templates 默认模板的唯一性改成「工程内唯一」（DB-009 第四批）
--
-- 背景：迁移 164 为 EDT-014 建的唯一索引是
--     idx_content_templates_default_per_type ON content_templates (entity_type) WHERE is_default = true
-- 它**不含 project_id**，于是「每个实体类型至多一个默认模板」是**全库**约束：
-- 多工程部署下第二个工程想给 article 标一个默认模板会直接撞唯一键（实测 23505），
-- 而报错信息只谈索引名，看不出「这是隔离粒度不对」。工程隔离下正确的粒度是
-- 「每个工程、每个实体类型至多一个默认模板」。
--
-- 顺序（PG 的约束）：**先建新索引、再删旧索引**。反过来的话，两条语句之间存在一段
-- 「没有任何唯一约束」的窗口 —— 并发插入会落进两行默认模板，而新索引随后建不起来
-- （迁移失败，库里已经脏了）。
--
-- 幂等与安全性：
--   · 判定按 pg_indexes 的 indexname，且**必须限定 schemaname**（pg_indexes 是全库的，
--     并发测试或库里的残留 schema 会让同名索引被一并查到）；
--   · 建索引前先探测「同工程同类型多行 is_default」：旧索引保证这不可能，但旧索引若被
--     手工删除过、或 164 的存量回填出过重复，这里的 DDL 会在启动路径上失败并卡住迁移器。
--     有冲突时**只报告不执行**（RAISE NOTICE 列出现状），判断留给人工。
--   · 旧索引只在新索引确实存在之后才 DROP。
--
-- 注册：public/migrations/register_analytics.go（与 164 同表，放在同一段）。
-- ========================================

DO $$
DECLARE
    conflicts    int;
    rows_detail  text;
BEGIN
    IF to_regclass('content_templates') IS NULL THEN
        RETURN;
    END IF;

    -- 冲突探测：同 (project_id, entity_type) 下多行 is_default。
    SELECT COUNT(*) INTO conflicts FROM (
        SELECT project_id, entity_type
        FROM content_templates
        WHERE is_default = true
        GROUP BY project_id, entity_type
        HAVING COUNT(*) > 1
    ) t;
    IF conflicts > 0 THEN
        SELECT string_agg(format('project=%s entity_type=%s n=%s', project_id, entity_type, n), '; ')
          INTO rows_detail
          FROM (
              SELECT project_id, entity_type, COUNT(*) AS n
              FROM content_templates
              WHERE is_default = true
              GROUP BY project_id, entity_type
              HAVING COUNT(*) > 1
          ) t;
        RAISE NOTICE '223: 检测到 % 组「同工程同类型多行默认模板」冲突，跳过索引重建：%',
            conflicts, rows_detail;
        RETURN;
    END IF;

    -- 1) 先建新索引：(project_id, entity_type) WHERE is_default。
    IF NOT EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND indexname = 'idx_content_templates_default_per_project_type'
    ) THEN
        EXECUTE 'CREATE UNIQUE INDEX idx_content_templates_default_per_project_type ' ||
                'ON content_templates (project_id, entity_type) WHERE is_default = true';
    END IF;

    -- 2) 再删旧索引（全库唯一，与工程隔离的粒度不符）。
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND indexname = 'idx_content_templates_default_per_type'
    ) THEN
        EXECUTE 'DROP INDEX idx_content_templates_default_per_type';
    END IF;
END $$;
