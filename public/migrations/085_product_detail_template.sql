-- 085_product_detail_template.sql — 默认商品详情内容模板（issue #6）。
--
-- 定位（spec #2「商品展示资产的落点」）：商品详情页是**内容模板（完整文档层）**，
-- 不是区块——它不参与全局块的 stale 传播，改版式只影响使用它的商品。
-- 模板内部**可以**引用全局区块（页眉/页脚等），构建期内联展开（docs/02-D §1.2）。
--
-- 本迁移只种一份类型级默认模板（entity_type='product'）：
--   · 发布实例按实体类型解析模板（ResolveTemplate），因此**新建商品即可用上**
--     这套模板，不需要为每个商品手工拼装；同一套模板复用于任意商品，换商品只换数据；
--   · 文档内容 = 一个 section 容器 + core.product 商品详情组件（issue #6 新增），
--     组件用命名槽位声明它需要的商品字段（标题/副标题/主图/图集/价格/划线价/描述），
--     构建期由商品解析器按白名单静态填入；
--   · 段落走 JSON 字面量，字段白名单的唯一来源是 product 模块 contract
--     （见 internal/module/product/contract/product_entity.go），这里只是一份数据。
--
-- 幂等：DO 块内先判「是否已有 product 模板」，有则直接返回；无工程时也直接返回
-- （下次启动重试）。注册：public/migrations/register.go（Seed 085-product-detail-template）。

DO $$
DECLARE
    v_project uuid;
    v_tpl     uuid := gen_random_uuid();
    v_ver     uuid := gen_random_uuid();
    v_doc     jsonb := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd-section","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"pd-body","type":"core.product","props":{"source":"product","mediaField":"product.defaultImage","galleryField":"product.images","titleField":"product.name","subtitleField":"product.subtitle","priceField":"product.priceRange","comparePriceField":"product.comparePrice","descriptionField":"product.description","currency":"¥","titleTag":"h1"}}]}]}'::jsonb;
BEGIN
    IF EXISTS (SELECT 1 FROM content_templates WHERE entity_type = 'product') THEN
        RETURN;
    END IF;
    SELECT id INTO v_project FROM projects ORDER BY created_at LIMIT 1;
    IF v_project IS NULL THEN
        RETURN;
    END IF;
    INSERT INTO content_templates (id, project_id, name, entity_type, draft_document, draft_version, current_version_id, created_at, updated_at)
    VALUES (v_tpl, v_project, '商品详情页', 'product', v_doc, 1, v_ver, now(), now());
    INSERT INTO content_template_versions (id, template_id, version, document, source_hash, created_by, created_at)
    VALUES (v_ver, v_tpl, 1, v_doc, encode(sha256(v_doc::text::bytea), 'hex'), '00000000-0000-0000-0000-000000000000'::uuid, now());
END $$;
