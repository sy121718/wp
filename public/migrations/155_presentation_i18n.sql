-- 155 · presentation 多语言产物（I18N-013，对齐 page_publications + page_artifacts.lang）
--
-- 背景：presentation 只构建默认语言，详情页无多语言产物；且 presentation_artifacts
-- UNIQUE(instance, version) 会让第二语言产物覆盖第一语言行。
-- 修法：
--   1) presentation_artifacts 加 lang + 唯一键 (presentation_instance_id, version, lang)；
--   2) presentation_publications 按 (presentation_id, lang) 记录各语言激活路径与产物指针；
--   3) presentation_instances.url_path 语义为**逻辑路径**（与 page.draft_path 同源），
--      回填时尽量剥掉语言前缀。
--
-- 幂等：IF NOT EXISTS / ADD COLUMN IF NOT EXISTS，重复执行安全。

-- 1) 每语言激活状态
CREATE TABLE IF NOT EXISTS presentation_publications (
    presentation_id uuid        NOT NULL REFERENCES presentation_instances(id) ON DELETE CASCADE,
    lang            text        NOT NULL,
    active_path     text        NOT NULL,
    artifact_id     uuid,
    artifact_hash   text        NOT NULL DEFAULT '',
    published_at    timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    PRIMARY KEY (presentation_id, lang)
);

COMMENT ON TABLE presentation_publications IS '自动发布实例每语言激活状态（I18N-013）';

-- 2) presentation_artifacts.lang
ALTER TABLE presentation_artifacts ADD COLUMN IF NOT EXISTS lang text;

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
    UPDATE presentation_artifacts SET lang = COALESCE(v_lang, 'zh-CN') WHERE lang IS NULL OR lang = '';
END $$;

ALTER TABLE presentation_artifacts ALTER COLUMN lang SET DEFAULT 'zh-CN';
ALTER TABLE presentation_artifacts ALTER COLUMN lang SET NOT NULL;

-- 3) 唯一键 (presentation_instance_id, version) → (presentation_instance_id, version, lang)
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT c.conname AS name, true AS is_constraint
          FROM pg_constraint c
          JOIN pg_class t ON t.oid = c.conrelid
         WHERE t.relname = 'presentation_artifacts'
           AND t.relnamespace = current_schema()::regnamespace
           AND c.contype = 'u'
           AND (SELECT array_agg(a.attname::text ORDER BY a.attname)
                  FROM pg_attribute a
                 WHERE a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)) = ARRAY['presentation_instance_id', 'version']
        UNION ALL
        SELECT i.relname AS name, false AS is_constraint
          FROM pg_index x
          JOIN pg_class i ON i.oid = x.indexrelid
          JOIN pg_class t ON t.oid = x.indrelid
         WHERE t.relname = 'presentation_artifacts'
           AND t.relnamespace = current_schema()::regnamespace
           AND x.indisunique
           AND NOT x.indisprimary
           AND NOT EXISTS (SELECT 1 FROM pg_constraint c2 WHERE c2.conindid = x.indexrelid)
           AND (SELECT array_agg(a.attname::text ORDER BY a.attname)
                  FROM pg_attribute a
                 WHERE a.attrelid = x.indrelid AND a.attnum = ANY (x.indkey)) = ARRAY['presentation_instance_id', 'version']
    LOOP
        IF r.is_constraint THEN
            EXECUTE format('ALTER TABLE presentation_artifacts DROP CONSTRAINT IF EXISTS %I', r.name);
        ELSE
            EXECUTE format('DROP INDEX IF EXISTS %I', r.name);
        END IF;
    END LOOP;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uk_presentation_artifacts_instance_version_lang
    ON presentation_artifacts (presentation_instance_id, version, lang);

COMMENT ON COLUMN presentation_artifacts.lang IS '构建语言（I18N-013）：唯一键第三维';

-- 4) 回填 publication 行 + 逻辑路径（尽力剥前缀，失败则原样保留）
DO $$
DECLARE
    v_default text := 'zh-CN';
BEGIN
    IF to_regclass('presentation_instances') IS NULL THEN
        RETURN;
    END IF;
    BEGIN
        SELECT COALESCE(NULLIF(TRIM(settings ->> 'defaultLang'), ''), NULLIF(TRIM(settings ->> 'default_lang'), ''), 'zh-CN')
          INTO v_default
          FROM projects
         ORDER BY created_at
         LIMIT 1;
    EXCEPTION WHEN undefined_table OR undefined_column OR datatype_mismatch THEN
        v_default := 'zh-CN';
    END;

    INSERT INTO presentation_publications (presentation_id, lang, active_path, artifact_id, artifact_hash, published_at, updated_at)
    SELECT i.id, v_default, i.url_path, i.active_artifact_id, COALESCE(a.artifact_hash, ''), COALESCE(i.published_at, i.updated_at), i.updated_at
      FROM presentation_instances i
      LEFT JOIN presentation_artifacts a ON a.id = i.active_artifact_id
     WHERE i.active_artifact_id IS NOT NULL
    ON CONFLICT (presentation_id, lang) DO NOTHING;
END $$;
