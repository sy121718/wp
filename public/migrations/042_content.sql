-- 042_content.sql — CMS 内容实体（docs/02-domain.md §1，0-A2 content 模块）。
-- 固定 CMS 内容（Article）与单调 revision：
--   - entity_type 判别（仅 article；商品/分类归领域模块，见迁移 080）
--   - slug 规范化公开路径段（同类型内唯一）
--   - revision 单调递增（内容变更 → 依赖追踪 → 触发关联实例重建）
--   - data 存实体字段（jsonb，字段白名单由 content service 校验，
--     见 internal/module/content/service 的 fieldWhitelist）

CREATE TABLE IF NOT EXISTS contents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type = 'article'),
    slug        text NOT NULL,
    revision    bigint NOT NULL DEFAULT 1,
    data        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- 同类型内 slug 唯一（URL 路径段）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_contents_type_slug ON contents(entity_type, slug);

COMMENT ON TABLE contents IS 'CMS 内容实体（0-A2 content 模块，docs/02-domain.md）';
