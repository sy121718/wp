-- 649 · 库存流水的 product_id / variant_id 改为可空（B2-2 收尾）。
--
-- 根因：迁移 641 之后，货行的身份是「仓库 × 仓库 SKU」，variant_id / product_id
-- 退化成线索列并且**允许为空**（台账页 /admin/inventory/skus 直接建的货行就没有归属，
-- 129 之后的「去认领」也是这个形态）。但流水表这两列还是 NOT NULL：
-- 一条无归属货行发生变动时写流水会写空串进 uuid 列，报
--   ERROR: invalid input syntax for type uuid: "" (SQLSTATE 22P02)
-- 也就是说「上一批把货行改成可无归属」时漏了流水侧的同一件事。
--
-- 为什么不改写成「给流水填一个占位 uuid」：流水是审计，占位值会被当成真实指向；
-- 而 NULL + stock_id（迁移 640）已经足够回答「动的是哪一条货」——
-- 归属本来就在 product_variants.warehouse_skus 里，流水不必复制一份可能过期的。
--
-- 外键不用动：product_id 上的 fk_inventory_movements_product 对 NULL 不检查。
ALTER TABLE inventory_stock_movements ALTER COLUMN product_id DROP NOT NULL;
ALTER TABLE inventory_stock_movements ALTER COLUMN variant_id DROP NOT NULL;

COMMENT ON COLUMN inventory_stock_movements.product_id IS
    '商品 id；可空（迁移 649）—— 无归属货行（台账页直接建的行）的流水没有商品指向，归属看 stock_id';
COMMENT ON COLUMN inventory_stock_movements.variant_id IS
    '变体 id；可空（迁移 649）—— 同上，仅作线索列，真正的归属在 product_variants.warehouse_skus';
