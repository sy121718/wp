-- 当前词条回补源；历史原文 264_product_warehouse_stock_i18n.sql 保持归档，不再执行。
-- 当前词条回补源；历史原文 264_product_warehouse_stock_i18n.sql 保持归档，不再执行。
-- 264 · 商品侧第一批（仓库侧裸码 / 多仓与认领复用 / 无限库存 / 主体 SKU 唯一性预检）的词条。
--
-- 本批新增的 enums 常量值就是 i18n key，真文案在这张表里；缺词条的后果是页面上原样显示
-- ErrContainerSKUTaken 这种裸 key（既不中文也不是话），所以新增常量必须同批 seed。
--
-- 两类：
--   · 一条业务错误（ErrContainerSKUTaken）—— 主体 SKU 在工程内被别的商品占用（迁移 246 的
--     偏唯一索引）。预检与唯一索引兜底都映射到它，错误里绝不含索引名 / SQLSTATE（CQ-009）；
--   · 商品列表与新建抽屉的新文案位 —— 库存三态（∞ / 数字 / 未入库 + 展开分仓）、
--     SKU 输入框的「认领 / 新建」两种预览、多仓勾选与数量（不填 = 无限）的说明。
--     中文站点有模板兜底看起来正常，英文站点会退回中文 —— 真源是本表，所以一并补上。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
-- 注：admin.products.col.stock 已由迁移 191 seed（本次复用，不重复登记）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pBoxSkuTaken', 'zh-CN', '该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('pBoxSkuTaken', 'en-US', 'This container SKU is already used by another product in this project (the container code is unique per project)', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('pStockInf', 'zh-CN', '∞ 无限', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockInf', 'en-US', '∞ Unlimited', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockInfShort', 'zh-CN', '∞', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockInfShort', 'en-US', '∞', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockNone', 'zh-CN', '未入库', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockNone', 'en-US', 'Not stocked yet', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockExpand', 'zh-CN', '展开分仓库存', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pStockExpand', 'en-US', 'Expand per-warehouse stock', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.reuseLabel', 'zh-CN', '该仓已有这条编码，将复用那一行（不新建）：', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.reuseLabel', 'en-US', 'This code already exists in the warehouse; that row will be reused (nothing new created): ', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.newLabel', 'zh-CN', '新编码，将在勾选的每个仓各建一行：', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.newLabel', 'en-US', 'New code; one row per selected warehouse: ', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pWhs', 'zh-CN', '建在哪些仓（可多选，不勾即默认仓）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pWhs', 'en-US', 'Warehouses to stock (multi-select; none = default warehouse)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pWhsHint', 'zh-CN', '按本表顺序第一个勾中的仓是认领仓（决定主体 SKU 的仓码前缀）；仓库里已有同编码的行会复用、不新建。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pWhsHint', 'en-US', 'The first checked warehouse in this list is the claiming one (it sets the container SKU prefix); an existing row with the same code is reused, not duplicated.', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pQtyTrack', 'zh-CN', '跟踪数量（不勾 = 无限）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pQtyTrack', 'en-US', 'Track quantity (unchecked = unlimited)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pQty', 'zh-CN', '不跟踪时留空（无限）；跟踪时填数量，0 = 没货', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pQty', 'en-US', 'Leave empty when untracked (unlimited); enter a number when tracked, 0 = out of stock', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pQtyHint', 'zh-CN', '不填数量 = 不跟踪 = 无限；显式填数字 = 跟踪并写入该数量（0 是合法的明确值）。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('pQtyHint', 'en-US', 'No quantity means untracked (unlimited); an explicit number means tracked and written as-is (0 is a valid, explicit value).', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pBoxSkuTaken', 'ErrContainerSKUTaken')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
