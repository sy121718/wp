-- 045_blueprint.sql — Page 初始化工具 Blueprint（docs/02-domain.md §1.2，0-B）。
-- Blueprint 是创建 Page Document 的版本化初始化输入：用完即弃（创建 Page
-- 时复制 AST 递归生成新 Node ID，后续修改不传播、不参与构建期）。
-- draft_document 可继续编辑，发布产生不可变版本写入 blueprint_versions。

CREATE TABLE IF NOT EXISTS blueprints (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    kind           text NOT NULL,
    draft_document jsonb NOT NULL DEFAULT '{}'::jsonb,
    draft_version  bigint NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS blueprint_versions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    blueprint_id uuid NOT NULL REFERENCES blueprints(id) ON DELETE CASCADE,
    version     bigint NOT NULL,
    document    jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_blueprint_version ON blueprint_versions(blueprint_id, version);

COMMENT ON TABLE blueprints IS 'Page 初始化工具 Blueprint（0-B，docs/02 §1.2）';
COMMENT ON TABLE blueprint_versions IS 'Blueprint 不可变版本快照';
