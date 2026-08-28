-- 020_themes.sql — 多主题体系:主题表 + 页面挂接主题
-- 主题 = 站点前端项目(全局颜色/字体/页眉/页脚引用/布局参数),一次一套激活。
-- 页面挂在主题下;切换激活主题 = 整站前端换皮,页面内容不动。

CREATE TABLE IF NOT EXISTS themes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        text NOT NULL,
    settings    jsonb NOT NULL DEFAULT '{}'::jsonb,
    is_active   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 同工程内主题名唯一。
CREATE UNIQUE INDEX IF NOT EXISTS uq_themes_project_name ON themes(project_id, name);

-- 页面挂接主题(可空=历史数据,回填激活主题)。
ALTER TABLE pages ADD COLUMN IF NOT EXISTS theme_id uuid REFERENCES themes(id) ON DELETE SET NULL;

-- 为尚无主题的工程补一套「默认主题」并激活(幂等:仅当工程没有任何主题时插入)。
INSERT INTO themes (id, project_id, name, settings, is_active)
SELECT gen_random_uuid(), p.id, '默认主题', '{}'::jsonb, true
FROM projects p
WHERE NOT EXISTS (SELECT 1 FROM themes t WHERE t.project_id = p.id);

-- 回填页面挂接:theme_id 为空的页面挂到所属工程当前激活主题。
UPDATE pages pg SET theme_id = t.id
FROM themes t
WHERE pg.project_id = t.project_id AND t.is_active AND pg.theme_id IS NULL;

-- 主题设置页脚注:settings 结构(与 Elementor Site Settings 对齐但单一结构):
-- {
--   "colors": { "primary": "", "text": "", "background": "", "surface": "", "border": "" },
--   "fontFamily": "",
--   "headerPageId": "",   // 全局页眉(引用本主题下一个页面的 id)
--   "footerPageId": ""    // 全局页脚
-- }
