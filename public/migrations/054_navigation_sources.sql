-- 047_navigation_sources.sql — 导航项多来源（对齐 WP「外观 → 菜单」）。
-- 菜单项不再只是「文字 + 链接」：可以从页面/文章/产品/分类/全局块取，
-- 构建期由 NavigationResolver 解析出标题与 URL；custom 表示手填链接。
-- target 对齐 WP 的「在新标签页打开」。

ALTER TABLE navigations
    ADD COLUMN IF NOT EXISTS source_type text NOT NULL DEFAULT 'custom'
        CHECK (source_type IN ('custom', 'page', 'article', 'product', 'category', 'block')),
    ADD COLUMN IF NOT EXISTS source_id uuid NULL,
    ADD COLUMN IF NOT EXISTS target text NOT NULL DEFAULT 'self'
        CHECK (target IN ('self', 'blank'));

CREATE INDEX IF NOT EXISTS idx_navigations_source ON navigations(source_type, source_id);

COMMENT ON COLUMN navigations.source_type IS '菜单项来源：custom/page/article/product/category/block';
COMMENT ON COLUMN navigations.source_id IS '来源实体 ID（custom 时为空，直接用 title/path）';
COMMENT ON COLUMN navigations.target IS '打开方式：self（当前窗口）/ blank（新窗口）';
