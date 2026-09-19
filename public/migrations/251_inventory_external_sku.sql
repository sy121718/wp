-- 251 · 仓库侧外部编码（inventory_stocks.external_sku）+ 按外码反查的普通索引。
--
-- 口径（docs/14-product-sku-and-cost-model.md §9.3，2026-09-19 用户补充确认）：
--   · 属性属于**商品**（属性组 + 值 → 笛卡尔积 → variant.option_values），仓库侧只回答
--     「这条货在这个仓叫什么」；
--   · 本列就是那个「叫什么」：这一行库存**在该仓**的外部 / 第三方编码。
--     空串 = 该仓用我们自己的 SKU（自营仓的常态）；非空 = 第三方仓 / 平台仓的编码；
--   · 映射是 **N:1**（多个 variant → 一个外部码）：同一个商品的十几个口味在仓库侧
--     可能共用同一个价格 / 同一条 SKU。因此这里**不能**建唯一索引 ——
--     UNIQUE (warehouse_id, external_sku) 会把「12 个口味共用 1 个外码」这种合法数据
--     判成冲突（该形态一度写进设计，已按用户口径改掉）；
--   · 唯一性改由 service 做**弱校验**：同一个 external_sku 在同一个仓库内必须指向
--     **同一个 product_id** —— 多口味共用合法；两个不同**商品**共用一个外码报业务错误
--     （inventoryenums.ErrExternalSKUProductConflict）；
--   · UNIQUE (warehouse_id, sku_code)（我们自己的 SKU 仓内唯一，迁移 244）保持不动：
--     它管的是「我们的编码」，与本列无关，两者互不干扰。
--
-- 为什么是普通索引：本列只用于「按外码反查这条货」，查得动即可。把 N:1 的关系固化成
-- 1:1 的约束，等于让数据库去否认真实的业务形态 —— 冲突不是脏数据，是设计写错了。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + CREATE INDEX IF NOT EXISTS。
ALTER TABLE inventory_stocks ADD COLUMN IF NOT EXISTS external_sku text NOT NULL DEFAULT '';

COMMENT ON COLUMN inventory_stocks.external_sku IS '这条库存在该仓的外部 / 第三方编码（N:1：同一商品的多个变体可共用；空串 = 该仓用我们自己的 SKU）';

CREATE INDEX IF NOT EXISTS idx_inventory_stocks_warehouse_external_sku
    ON inventory_stocks (warehouse_id, external_sku);
