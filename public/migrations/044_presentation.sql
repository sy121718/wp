-- 044_presentation.sql — 自动发布实例与文档快照（docs/02-domain.md §3，0-A2）。
-- PresentationInstance 是内容实体的自动发布页面实例，与手工 Page 共享
-- 编译/存储/激活管线；DocumentSnapshot 保存已解析 AST（构建期内容固化）。

CREATE TABLE IF NOT EXISTS presentation_instances (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type         text NOT NULL CHECK (entity_type IN ('product', 'article', 'category')),
    entity_id           uuid NOT NULL,
    url_path            text NOT NULL,
    status              text NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'archived')),
    current_snapshot_id uuid NULL,
    artifact_hash       text NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- URL 路径唯一（与手工 Page 共享 URL 占用语义）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_presentation_url_path ON presentation_instances(url_path);

CREATE TABLE IF NOT EXISTS document_snapshots (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    presentation_instance_id uuid NOT NULL REFERENCES presentation_instances(id) ON DELETE CASCADE,
    source_template_version_id uuid NOT NULL,
    source_entity_revision    bigint NOT NULL,
    document                 jsonb NOT NULL,
    created_at               timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_snapshots_instance ON document_snapshots(presentation_instance_id);

COMMENT ON TABLE presentation_instances IS '内容实体自动发布实例（0-A2，docs/02-domain.md §3）';
COMMENT ON TABLE document_snapshots IS '已解析 AST 快照（构建期内容固化）';
