-- 164_content_template_is_default.sql — EDT-014：content_templates 显式默认模板标记。
--
-- 每个 entity_type 至多一个 is_default=true（部分唯一索引兜底）；
-- 存量：各类型 updated_at 最新的一条标为默认（尚无默认时）。

ALTER TABLE content_templates
    ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT false;

UPDATE content_templates t
SET is_default = true
FROM (
    SELECT DISTINCT ON (entity_type) id
    FROM content_templates
    ORDER BY entity_type, updated_at DESC, id DESC
) pick
WHERE t.id = pick.id
  AND NOT EXISTS (
      SELECT 1 FROM content_templates x
      WHERE x.entity_type = t.entity_type AND x.is_default = true
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_templates_default_per_type
    ON content_templates (entity_type)
    WHERE is_default = true;
