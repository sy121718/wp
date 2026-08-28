-- 021_blocks.sql — 全局块体系（方案 C：提前轻量落地 0-B Global Component 核心）
-- 全局块 = 跨页面复用的结构片段（无 URL、不进路由），对应 WP 的 synced pattern / template part：
--   kind=block   普通全局块（未来页面文档 globalRef 引用）
--   kind=header  页眉候选（主题 settings.headerBlockId 绑定）
--   kind=footer  页脚候选（主题 settings.footerBlockId 绑定）
-- 页面构建时编译期内联绑定块（访问面保持纯静态），块内容 hash 计入产物输入。

CREATE TABLE IF NOT EXISTS blocks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        text NOT NULL,
    kind        text NOT NULL DEFAULT 'block',
    document    jsonb NOT NULL DEFAULT '{"settings":{"layout":{"mode":"full"}},"root":[]}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 同工程内块名唯一。
CREATE UNIQUE INDEX IF NOT EXISTS uq_blocks_project_name ON blocks(project_id, name);
