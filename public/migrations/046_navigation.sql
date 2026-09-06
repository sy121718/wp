-- 046_navigation.sql — 公开站点导航（docs/05 阶段6，0-C）。
-- navigation 与后台权限菜单 menu 严格隔离（命名约束：menu=后台权限菜单，
-- navigation=公开站点导航）。kind 区分页眉/页脚导航；parent_id 支持层级。

CREATE TABLE IF NOT EXISTS navigations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title      text NOT NULL,
    path       text NOT NULL,
    kind       text NOT NULL DEFAULT 'header' CHECK (kind IN ('header', 'footer')),
    parent_id  uuid NULL REFERENCES navigations(id) ON DELETE CASCADE,
    sort_order int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_navigations_project ON navigations(project_id, kind, sort_order);

COMMENT ON TABLE navigations IS '公开站点导航（0-C，与后台 menu 严格隔离）';
