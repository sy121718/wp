-- 622 · 仓库商品 SKU 主档化（迁移 633）的文案词条：中英成对。
--
-- 本批新增两类取词位：
--
--   ① 业务错误（enums 常量值即 i18n key，不 seed 就会在页面上原样显示裸 key）：
--      · ErrStockAlreadyClaimed —— 认领冲突：目标 (变体, 仓) 已有库存行；
--      · ErrStockHasQuantity  —— 带数量 / 预占的行不能解除认领；
--      · ErrStockNotClaimed   —— 这条存货还没认领（或已经认领过了）；
--      · ErrReserveUnderflow  —— 释放的预占量超过已预占量（不静默夹到 0）。
--
--   ② 仓库商品 SKU 台账页（/admin/inventory/skus）的用户可见文案：
--      页面标题与说明、八列表头、筛选条、空态、认领 / 解除认领抽屉、
--      新建存货抽屉，「已认领 / 未认领」两个徽标。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
--
-- 判定阈值见 register_catalog.go 的注册项：逐条枚举本批 (item_key, lang) 组合数，
-- 不用全库总量、也不用前缀 LIKE（前者会被同期其它批次的行满足而静默跳过，
-- 后者会被将来新增的同前缀 key 带跑 —— 仓库里 226/277/283/592 都踩过）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrStockAlreadyClaimed', 'zh-CN', '这个商品变体在该仓已经有库存行了：两行库存是两份事实，不能自动合并，请先核对数量与成本再人工处理', 409, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrStockAlreadyClaimed', 'en-US', 'This variant already has a stock row in that warehouse; two stock rows are two facts and are never merged automatically — check the quantities and costs first', 409, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrStockHasQuantity', 'zh-CN', '这条存货还有数量或预占，不能解除认领：请先把数量清零、预占释放完', 409, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrStockHasQuantity', 'en-US', 'This stock row still has quantity or reservations and cannot be unclaimed; zero the quantity and release the reservations first', 409, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrStockNotClaimed', 'zh-CN', '这条存货还没有认领到商品变体（或者已经认领过了）', 400, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrStockNotClaimed', 'en-US', 'This stock row is not claimed to a product variant (or is already claimed)', 400, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrReserveUnderflow', 'zh-CN', '要释放的预占数量超过了这一行已预占的数量', 409, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('ErrReserveUnderflow', 'en-US', 'The quantity to release exceeds what this row has reserved', 409, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.title', 'zh-CN', '仓库商品 SKU', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.title', 'en-US', 'Warehouse stock SKUs', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.lead', 'zh-CN', '仓库里的货可以先于商品存在：一条编码、一个数量、一个成本，暂时还不知道是哪个商品。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.lead', 'en-US', 'Stock can exist in the warehouse before the product does: a code, a quantity, a cost, and no product yet.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.mid', 'zh-CN', '认领之后这条货就挂到商品变体上，之后的预占、发货、流水全部照旧。可售 = 数量 − 预占，预占是已经留给未发货订单的那部分。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.mid', 'en-US', 'Once claimed, the row belongs to a product variant and reservations, shipping and movements all work as usual. Available = quantity − reserved, where reserved is what unshipped orders already hold.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.boundary', 'zh-CN', '仓库里的编码永远是裸码（不带仓码前缀）；前缀只属于商品侧。调拨、发货这些有单据的动货走各自单据，本页只维护「这条货是谁的」。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.boundary', 'en-US', 'Warehouse codes are always bare codes (no warehouse prefix); the prefix belongs to the product side. Document-driven movements (transfers, shipping) stay in their own documents — this page only maintains which product a row belongs to.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createTitle', 'zh-CN', '新建存货（未认领）', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createTitle', 'en-US', 'New stock row (unclaimed)', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.create', 'zh-CN', '新建存货', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.create', 'en-US', 'New stock row', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.listLead', 'zh-CN', '仓库里的货（', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.listLead', 'en-US', 'Stock rows (', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.claimed', 'zh-CN', '认领', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.claimed', 'en-US', 'Claimed', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.reserved', 'zh-CN', '预占', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.reserved', 'en-US', 'Reserved', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.available', 'zh-CN', '可售', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.available', 'en-US', 'Available', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.cost', 'zh-CN', '成本价', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.col.cost', 'en-US', 'Cost price', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.filter.keyword', 'zh-CN', '关键词', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.filter.keyword', 'en-US', 'Keyword', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.filter.keywordPh', 'zh-CN', 'SKU 或外部编码', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.filter.keywordPh', 'en-US', 'SKU or external code', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.emptyHeading', 'zh-CN', '还没有仓库商品 SKU', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.emptyHeading', 'en-US', 'No warehouse stock SKUs yet', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.emptyDesc', 'zh-CN', '仓库里有货但还没建商品时，用「新建存货」先把货记下来，之后再来认领。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.emptyDesc', 'en-US', 'When goods arrive before the product exists, record them with "New stock row" and claim them later.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.unclaimConfirm', 'zh-CN', '解除认领？只有数量与预占都为 0 的存货行可以解除。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.unclaimConfirm', 'en-US', 'Unclaim? Only rows with zero quantity and zero reservations can be unclaimed.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.unclaim', 'zh-CN', '解除认领', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.unclaim', 'en-US', 'Unclaim', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claim', 'zh-CN', '认领', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claim', 'en-US', 'Claim', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimTitle', 'zh-CN', '认领到商品变体', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimTitle', 'en-US', 'Claim to a product variant', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimTarget', 'zh-CN', '认领到变体', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimTarget', 'en-US', 'Claim to variant', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimPick', 'zh-CN', '选择一个商品变体', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimPick', 'en-US', 'Pick a product variant', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimSubmit', 'zh-CN', '确认认领', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.claimSubmit', 'en-US', 'Claim', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createWarehousePh', 'zh-CN', '选择仓库', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createWarehousePh', 'en-US', 'Pick a warehouse', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createCodePh', 'zh-CN', '仓库侧裸码，如 DRAWERSMOKE_001（不带仓码前缀）', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createCodePh', 'en-US', 'Bare warehouse code, e.g. DRAWERSMOKE_001 (no warehouse prefix)', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.costPh', 'zh-CN', '留空 = 尚未核算', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.costPh', 'en-US', 'Empty = not costed yet', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createSubmit', 'zh-CN', '建好这条存货', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.createSubmit', 'en-US', 'Create the stock row', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.stock.claimed', 'zh-CN', '已认领', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.stock.claimed', 'en-US', 'Claimed', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.stock.unclaimed', 'zh-CN', '未认领', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.stock.unclaimed', 'en-US', 'Unclaimed', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.filter.all', 'zh-CN', '（全部）', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.filter.all', 'en-US', '(all)', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.filter.submit', 'zh-CN', '查询', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.inventory.skus.filter.submit', 'en-US', 'Search', 200, 'inventory', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
