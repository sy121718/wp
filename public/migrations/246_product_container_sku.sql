-- 246 · 商品容器主体 SKU（products.sku_code）与项目内唯一索引
--
-- 为什么需要这一列：新 SKU 规则下「容器主体」是商品的对外身份 ——
--   · 变体商品：从仓库选时 = <仓库短码>_<仓库里那条 SKU>；自定义时（选了仓库）自动附加仓库码；
--   · 捆绑商品：自定义，以 _B 结尾。
-- 但 products 表此前只存 name/slug/default_price，**没有 SKU 列**：捆绑商品自 238 起不再生成
-- 首个变体，其主体 SKU 无处可放；变体商品的「主体」同样没有落点（变体行只存变体自己的 SKU）。
-- 这一列就是容器主体的唯一落点。
--
-- 唯一性口径（用户 2026-09-19 确认，见 docs/14 §4）：
--   · 商品内唯一：product_variants 已有 UNIQUE (product_id, sku_code)，保留；
--   · 仓库内唯一：同一仓库不能有重复 SKU —— 由 inventory_stocks 的 UNIQUE (warehouse_id, sku_code)
--     保证（另批），商品侧不重复加校验；
--   · **容器主体唯一**：本索引 UNIQUE (project_id, sku_code)；
--   · 捆绑成员选到同一个 SKU 不做额外约束（成员不是身份，两个捆绑可以共用同一个成员 SKU）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + CREATE UNIQUE INDEX IF NOT EXISTS。
-- 偏索引 WHERE sku_code <> ''：存量商品还没有主体 SKU，空串不参与唯一约束（可随后续编辑逐个补齐）。
ALTER TABLE products ADD COLUMN IF NOT EXISTS sku_code text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_products_project_sku_code
    ON products (project_id, sku_code)
    WHERE sku_code <> '';
