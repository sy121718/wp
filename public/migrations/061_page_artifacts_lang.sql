-- 061 · page_artifacts 加 lang 维度（多语言上线第一阻塞项，docs/06-D-site-i18n.md §15.5 第 1 条）
--
-- 背景：page_artifacts UNIQUE(page_id, version) 让「同一页面同一草稿版本」只能存在一行产物，
-- 第二个语言的产物会替换第一个语言的行 —— 先登记的路由 page_routes.artifact_id 指向错内容。
-- 修法：加 lang 列（存量行回填站点默认语言）+ 唯一键改 (page_id, version, lang)。
--
-- 幂等：ADD COLUMN IF NOT EXISTS / DROP ... IF EXISTS / CREATE UNIQUE INDEX IF NOT EXISTS，
-- 重复执行安全。注册见 register.go：CheckSQL 按 lang 列是否存在判定，
-- 避免默认的「表存在即跳过」把本迁移误跳过（page_artifacts 由 002-init-builder-schema 创建）。
--
-- 运维提示：DROP/ADD 唯一键会短暂持有 ACCESS EXCLUSIVE 锁；page_artifacts 是元数据表、
-- 体量可控，直接执行即可。不用 CONCURRENTLY：迁移器逐条 Exec 无显式事务，但并发建索引
-- 失败会留下 INVALID 索引，而 IF NOT EXISTS 之后会误跳过，风险大于收益。

-- 1) 加 lang 列：先允许 NULL，便于回填存量行（回填后立即 SET NOT NULL）。
ALTER TABLE page_artifacts ADD COLUMN IF NOT EXISTS lang text;

-- 2) 回填存量行：站点默认语言。
--    迁移器读不到 config.yaml 的 i18n.default_lang，故按「项目设置声明 → 应用内置默认 zh-CN」
--    两级取值，与 pkg/i18n.fallbackDefaultLang 及 config.yaml 默认值一致；
--    projects.settings 异常（表缺失/非 jsonb）时静默回退，绝不阻断迁移。
DO $$
DECLARE
    v_lang text;
BEGIN
    BEGIN
        SELECT COALESCE(NULLIF(TRIM(settings ->> 'defaultLang'), ''), NULLIF(TRIM(settings ->> 'default_lang'), ''))
          INTO v_lang
          FROM projects
         WHERE COALESCE(NULLIF(TRIM(settings ->> 'defaultLang'), ''), NULLIF(TRIM(settings ->> 'default_lang'), '')) IS NOT NULL
         ORDER BY created_at
         LIMIT 1;
    EXCEPTION WHEN undefined_table OR undefined_column OR datatype_mismatch THEN
        v_lang := NULL;
    END;
    UPDATE page_artifacts SET lang = COALESCE(v_lang, 'zh-CN') WHERE lang IS NULL OR lang = '';
END $$;

-- 3) 新行兜底默认值 + 禁止空语言（空值会让新唯一键退化成互相覆盖）。
ALTER TABLE page_artifacts ALTER COLUMN lang SET DEFAULT 'zh-CN';
ALTER TABLE page_artifacts ALTER COLUMN lang SET NOT NULL;

-- 4) 删除旧的 (page_id, version) 唯一键：按「列组合」识别，不依赖约束名。
--    生产 DDL 内联 UNIQUE 生成的 page_artifacts_page_id_version_key、
--    GORM AutoMigrate 生成的 uk_page_version / uq_page_artifacts_page_version 一并清理；
--    UNIQUE(id, page_id)（page_dependencies 外键目标）列组合不同，保持不动。
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT c.conname AS name, true AS is_constraint
          FROM pg_constraint c
          JOIN pg_class t ON t.oid = c.conrelid
         WHERE t.relname = 'page_artifacts'
           AND t.relnamespace = current_schema()::regnamespace
           AND c.contype = 'u'
           AND (SELECT array_agg(a.attname::text ORDER BY a.attname)
                  FROM pg_attribute a
                 WHERE a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)) = ARRAY['page_id', 'version']
        UNION ALL
        SELECT i.relname AS name, false AS is_constraint
          FROM pg_index x
          JOIN pg_class i ON i.oid = x.indexrelid
          JOIN pg_class t ON t.oid = x.indrelid
         WHERE t.relname = 'page_artifacts'
           AND t.relnamespace = current_schema()::regnamespace
           AND x.indisunique
           AND NOT x.indisprimary
           AND NOT EXISTS (SELECT 1 FROM pg_constraint c2 WHERE c2.conindid = x.indexrelid)
           AND (SELECT array_agg(a.attname::text ORDER BY a.attname)
                  FROM pg_attribute a
                 WHERE a.attrelid = x.indrelid AND a.attnum = ANY (x.indkey)) = ARRAY['page_id', 'version']
    LOOP
        IF r.is_constraint THEN
            EXECUTE format('ALTER TABLE page_artifacts DROP CONSTRAINT IF EXISTS %I', r.name);
        ELSE
            EXECUTE format('DROP INDEX IF EXISTS %I', r.name);
        END IF;
    END LOOP;
END $$;

-- 5) 新建唯一键 (page_id, version, lang)：同页多语言各占一行、互不覆盖。
--    索引名与 model 的 gorm 标签（uniqueIndex:uk_page_artifacts_page_version_lang）一致，
--    保证 AutoMigrate 与生产 DDL 同名同形。
CREATE UNIQUE INDEX IF NOT EXISTS uk_page_artifacts_page_version_lang
    ON page_artifacts (page_id, version, lang);

-- 6) 列注释（对齐 055/056 的注释风格）。
COMMENT ON COLUMN page_artifacts.lang IS '构建语言（多语言 P3）：唯一键 (page_id, version, lang) 的第三维，禁止为空';
