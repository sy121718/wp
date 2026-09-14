-- 160 · presentation_instances.entity_type 与商品域实体类型对齐（DB-013）。
--
-- 历史 CHECK 只有 product/article/category；商品域用 product_category 等五类型。
-- 与 080 content_templates 一致：解掉 DDL 枚举，合法性由 Go 实体类型注册表判定。

ALTER TABLE presentation_instances DROP CONSTRAINT IF EXISTS presentation_instances_entity_type_check;

-- 存量 category → product_category（若存在）。
UPDATE presentation_instances SET entity_type = 'product_category' WHERE entity_type = 'category';
