-- 062 · page_publications：页面「每语言激活状态」（多语言 P3，docs/06-D-site-i18n.md §15.5 第 2 条）
--
-- 背景：pages.active_path 是单值，Publish(en-US) 把它当作「本页旧的激活路径」去
-- Deactivate，于是 /zh-CN/about 的 active 路由行被删除（DELETE 行）——「一页多语言
-- 同时在线」不成立。修法：激活状态按 (page_id, lang) 独立落表，Publish / Rollback /
-- UpdateURL / Deactivate 全部按语言作用域；pages.active_path 保留为「最近发布语言的
-- 单值镜像」，既有读取方（导航来源候选、页面列表投影）零改动。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / 回填 ON CONFLICT DO NOTHING，重复执行安全。
-- 注册见 register.go：本表为新表，用默认「表存在即跳过」检查即可。

CREATE TABLE IF NOT EXISTS page_publications (
    page_id       uuid        NOT NULL,
    lang          text        NOT NULL,
    active_path   text        NOT NULL,
    artifact_id   uuid,
    artifact_hash text        NOT NULL DEFAULT '',
    published_at  timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (page_id, lang)
);

-- 回填：既有 pages.active_path 记为站点默认语言那一行（迁移器读不到 config.yaml，
-- 口径与 061 一致：项目设置 defaultLang/default_lang → 应用内置 zh-CN）。
DO $$
DECLARE
    v_default text;
BEGIN
    IF to_regclass('pages') IS NULL THEN
        RETURN;
    END IF;
    BEGIN
        SELECT COALESCE(NULLIF(TRIM(settings ->> 'defaultLang'), ''), NULLIF(TRIM(settings ->> 'default_lang'), ''))
          INTO v_default
          FROM projects
         WHERE COALESCE(NULLIF(TRIM(settings ->> 'defaultLang'), ''), NULLIF(TRIM(settings ->> 'default_lang'), '')) IS NOT NULL
         ORDER BY created_at
         LIMIT 1;
    EXCEPTION WHEN undefined_table OR undefined_column OR datatype_mismatch THEN
        v_default := NULL;
    END;
    INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, updated_at)
    SELECT p.id, COALESCE(v_default, 'zh-CN'), p.active_path, p.active_artifact_id, '',
           COALESCE(p.published_at, p.updated_at), p.updated_at
      FROM pages p
     WHERE p.active_path IS NOT NULL AND p.active_path <> ''
    ON CONFLICT (page_id, lang) DO NOTHING;
END $$;

COMMENT ON TABLE page_publications IS '页面每语言激活状态（多语言 P3）：主键 (page_id, lang)，禁止空语言';
COMMENT ON COLUMN page_publications.artifact_hash IS '该语言当前激活产物的内容 hash（回滚/校验用）';
