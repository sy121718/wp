-- 087b_product_variant_options_template.sql — 默认商品详情模板补规格槽位（issue #8）。
--
-- 085 种的默认商品详情模板只声明了 issue #6 的槽位；issue #8 的规格选择器需要
-- optionsField / variantsField 两个槽位才会输出。085 是「首次建库才执行」的种子，
-- 已执行过的库（含开发库）不会重跑，故这里补一次幂等的数据迁移。
--
-- 安全边界：只重写**与 085 原样一致**的模板（jsonb 相等比较，与文本格式无关）——
-- 作者改过的模板一个字节都不动（缺槽位是作者的选择，改成什么样由作者决定）。
-- ConditionSQL 按仓库既有语义「已存在则跳过」：product 模板都带上 optionsField 时
-- 返回 1 跳过；仍有缺槽位的模板时返回 0 → 跑 DO 块。作者自定义的模板若一直不带
-- 槽位，本种子会每次启动都跑一次 DO 块：DO 块按相等匹配，匹配不到行，是空转。
--
-- 幂等：模板与版本各按相等匹配更新一次；补完之后条件成立（槽位已存在），不再改写。

DO $$
DECLARE
    v_old jsonb := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd-section","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"pd-body","type":"core.product","props":{"source":"product","mediaField":"product.defaultImage","galleryField":"product.images","titleField":"product.name","subtitleField":"product.subtitle","priceField":"product.priceRange","comparePriceField":"product.comparePrice","descriptionField":"product.description","currency":"¥","titleTag":"h1"}}]}]}'::jsonb;
    v_new jsonb := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd-section","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"pd-body","type":"core.product","props":{"source":"product","mediaField":"product.defaultImage","galleryField":"product.images","titleField":"product.name","subtitleField":"product.subtitle","priceField":"product.priceRange","comparePriceField":"product.comparePrice","descriptionField":"product.description","optionsField":"product.options","variantsField":"product.variants","currency":"¥","titleTag":"h1"}}]}]}'::jsonb;
BEGIN
    -- 版本行先改（草稿改完后 current_version_id 仍在，但版本自身的文档判定独立）。
    UPDATE content_template_versions v
    SET document = v_new,
        source_hash = encode(sha256(v_new::text::bytea), 'hex')
    FROM content_templates t
    WHERE v.id = t.current_version_id
      AND t.entity_type = 'product'
      AND v.document = v_old;

    UPDATE content_templates
    SET draft_document = v_new, updated_at = now()
    WHERE entity_type = 'product' AND draft_document = v_old;
END $$;
