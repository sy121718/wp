-- 641 · 货行身份从「变体 × 仓」改成「仓 × 仓库 SKU」。
--
-- 638 给流水加了货行指针（stock_id），641 把寻址本身改掉：库存行的身份不再是
-- 「哪个变体在这个仓有一行」（UNIQUE (variant_id, warehouse_id)，迁移 099），
-- 而是 DDL 上本就更贴仓库事实的 UNIQUE (warehouse_id, sku_code)（迁移 244）。
--
-- 为什么必须改：真实仓库里同一个变体在同一个仓可以有多条货 —— 自有码与客户码并存、
-- 不同批次分账、同一款货按供应商分开记。旧的唯一键把这种形态判成冲突（23505），
-- 于是只能拆成两个商品去绕过它。而「一条货供给多个变体」同样成立
-- （同仓同码供多规格 / 套装 / 多站点），这也是 637 把关联改成多对多的原因。
--
-- 本迁移只做结构：删掉那条唯一约束（以及随之无用的一条索引）。
-- 两列本身保留 —— 它们仍是「这条货卖出的是谁」的线索（收货 / 建品时写入），
-- 只是不再参与定位，也不再唯一。读路径按货行 id 或 (仓, 编码) 走。

-- UNIQUE (variant_id, warehouse_id)：641 的根因。
ALTER TABLE inventory_stocks
    DROP CONSTRAINT IF EXISTS inventory_stocks_variant_id_warehouse_id_key;

-- 该索引只为「按变体列全部仓」服务，而定位已改为按 id / 编码：留着只会让每次
-- 建行多写一份索引，且 variant_id 为 NULL 的行（无归属货）全都被索引进来。
DROP INDEX IF EXISTS idx_inventory_stocks_variant_project;

COMMENT ON COLUMN inventory_stocks.variant_id IS
    '这条货卖出 / 归属于哪个变体（线索列，641 起不再唯一、不参与定位）；真正的多对多关联在 product_variants.warehouse_skus';
COMMENT ON COLUMN inventory_stocks.product_id IS
    '这条货归属哪个商品（线索列，641 起不再唯一、不参与定位）';
