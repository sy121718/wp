-- 638 · 下线「认领」概念：未认领 / 已认领不再是两个状态，库存行与商品的对应关系
--       改由 product_variants.warehouse_skus（迁移 637）承担。
--
-- 本迁移只做**反向清理**（结构由 637 建立，代码侧已删）：
--   ① 删 625 seed 过的认领文案（4 条业务错误 + 28 个台账页界面位，中英成对）；台账页
--      本身随代码一起下线（/admin/inventory/skus 路由已删），词条留着只会在后台词条表里
--      积成一批没人用的 key。
--   ② 删 624 建的两个「未认领」专用索引（认领概念下线后 variant_id 不再有 NULL 语义）。
--      UNIQUE (variant_id, warehouse_id) 保留：它守的是「同一变体的同一仓只有一行货」，
--      与认领无关；variant_id 可空的放宽也保留（迁移的行仍需要它，回滚时不必重建）。
--
-- 幂等：DELETE 再跑一遍影响 0 行；DROP INDEX IF EXISTS。
DELETE FROM sys_i18n
WHERE item_key IN (
    'ErrStockAlreadyClaimed', 'ErrStockHasQuantity', 'ErrStockNotClaimed',
    'admin.inventory.skus.title', 'admin.inventory.skus.lead', 'admin.inventory.skus.mid',
    'admin.inventory.skus.boundary', 'admin.inventory.skus.createTitle', 'admin.inventory.skus.create',
    'admin.inventory.skus.listLead', 'admin.inventory.skus.col.claimed', 'admin.inventory.skus.col.reserved',
    'admin.inventory.skus.col.available', 'admin.inventory.skus.col.cost',
    'admin.inventory.skus.filter.keyword', 'admin.inventory.skus.filter.keywordPh',
    'admin.inventory.skus.emptyHeading', 'admin.inventory.skus.emptyDesc',
    'admin.inventory.skus.unclaimConfirm', 'admin.inventory.skus.unclaim', 'admin.inventory.skus.claim',
    'admin.inventory.skus.claimTitle', 'admin.inventory.skus.claimTarget', 'admin.inventory.skus.claimPick',
    'admin.inventory.skus.claimSubmit', 'admin.inventory.skus.createWarehousePh',
    'admin.inventory.skus.createCodePh', 'admin.inventory.skus.costPh', 'admin.inventory.skus.createSubmit',
    'admin.inventory.stock.claimed', 'admin.inventory.stock.unclaimed',
    'admin.inventory.skus.filter.submit'
);

DROP INDEX IF EXISTS idx_inventory_stocks_unclaimed;
