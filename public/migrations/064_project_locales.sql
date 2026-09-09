-- 064 · project_locales：站点语言清单（多语言 P3，docs/06-D-site-i18n.md §14 D10）
--
-- 真源作用：站点启用哪些语言、顺序、默认语言。消费方：
--   1) 路由登记（page 建页/改 URL 按每启用语言登记 page_routes 行）；
--   2) sitemap 按语言分组与 hreflang 互指；
--   3) 产物 head 的 hreflang（x-default 取 is_default）；
--   4) 语言切换器（后续 UI）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE UNIQUE INDEX IF NOT EXISTS /
-- 回填 ON CONFLICT DO NOTHING，重复执行安全。

CREATE TABLE IF NOT EXISTS project_locales (
    project_id uuid        NOT NULL,
    lang       text        NOT NULL,
    sort_order integer     NOT NULL DEFAULT 0,
    is_default boolean     NOT NULL DEFAULT false,
    enabled    boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, lang)
);

-- 每工程至多一个默认语言（部分唯一索引）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_project_locales_default
    ON project_locales (project_id) WHERE is_default;

-- 回填：既有工程各登记一行默认语言。迁移器读不到 config.yaml 的 i18n.default_lang，
-- 故按「项目设置 defaultLang/default_lang → 应用内置 zh-CN」两级取值（与 061/062 同口径）。
DO $$
BEGIN
    IF to_regclass('projects') IS NULL THEN
        RETURN;
    END IF;
    INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, created_at, updated_at)
    SELECT p.id,
           COALESCE(NULLIF(TRIM(p.settings ->> 'defaultLang'), ''), NULLIF(TRIM(p.settings ->> 'default_lang'), ''), 'zh-CN'),
           0, true, true, now(), now()
      FROM projects p
    ON CONFLICT DO NOTHING;
END $$;

COMMENT ON TABLE project_locales IS '站点语言清单（多语言 P3）：顺序 + 默认标记 + 启用状态';
COMMENT ON COLUMN project_locales.is_default IS '站点默认语言（每工程至多一行，hreflang x-default 取它）';
