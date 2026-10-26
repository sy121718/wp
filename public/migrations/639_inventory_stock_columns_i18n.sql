-- 639 · 库存页两处文案补 seed（去认领之后共用的取词位）。
--
-- 背景：/admin/inventory/skus 台账页下线（638）后，两个原本只在那页出现的取词位被
-- 库存页与调拨页承接：
--
--   · admin.inventory.sku.col.reserved / admin.inventory.sku.col.available —— 库存明细表的
--     「预占」「可售」两列（三量恒等式的两个分量）；
--   · admin.inventory.filter.keyword / admin.inventory.create.warehousePh —— 调拨页筛选条
--     的关键词标签与建单抽屉的仓库下拉占位。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.sku.col.reserved', 'zh-CN', '预占', 200, 'inventory', 'internal/templates/admin/inventory/inventory.html', 1, now(), now()),
('admin.inventory.sku.col.reserved', 'en-US', 'Reserved', 200, 'inventory', 'internal/templates/admin/inventory/inventory.html', 1, now(), now()),
('admin.inventory.sku.col.available', 'zh-CN', '可售', 200, 'inventory', 'internal/templates/admin/inventory/inventory.html', 1, now(), now()),
('admin.inventory.sku.col.available', 'en-US', 'Available', 200, 'inventory', 'internal/templates/admin/inventory/inventory.html', 1, now(), now()),
('admin.inventory.filter.keyword', 'zh-CN', '关键词', 200, 'inventory', 'internal/templates/admin/inventory/inventory_transfers.html', 1, now(), now()),
('admin.inventory.filter.keyword', 'en-US', 'Keyword', 200, 'inventory', 'internal/templates/admin/inventory/inventory_transfers.html', 1, now(), now()),
('admin.inventory.create.warehousePh', 'zh-CN', '选择仓库', 200, 'inventory', 'internal/templates/admin/inventory/inventory_transfers.html', 1, now(), now()),
('admin.inventory.create.warehousePh', 'en-US', 'Pick a warehouse', 200, 'inventory', 'internal/templates/admin/inventory/inventory_transfers.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;