-- 043_content_template.sql — 内容结构模板（docs/02-domain.md §2，0-A2）。
-- ContentTemplate 是版本化页面结构定义，参与每次构建派生 DocumentSnapshot
--（与 Blueprint 的「用完即弃」不同）。draft_document 可继续编辑，发布产生
-- 不可变版本写入 content_template_versions。

CREATE TABLE IF NOT EXISTS content_templates (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    entity_type    text NOT NULL CHECK (entity_type IN ('product', 'article', 'category')),
    draft_document jsonb NOT NULL DEFAULT '{}'::jsonb,
    draft_version  bigint NOT NULL DEFAULT 1,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- 同类型内模板名唯一。
CREATE UNIQUE INDEX IF NOT EXISTS uq_content_templates_type_name ON content_templates(entity_type, name);

CREATE TABLE IF NOT EXISTS content_template_versions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id uuid NOT NULL REFERENCES content_templates(id) ON DELETE CASCADE,
    version     bigint NOT NULL,
    document    jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- 模板内版本唯一。
CREATE UNIQUE INDEX IF NOT EXISTS uq_template_version ON content_template_versions(template_id, version);

COMMENT ON TABLE content_templates IS '内容结构模板（0-A2，docs/02-domain.md §2）';
COMMENT ON TABLE content_template_versions IS '模板不可变版本快照';
