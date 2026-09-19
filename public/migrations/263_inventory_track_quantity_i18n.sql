-- 263 · 无限库存（不跟踪数量）的新文案词条：中英成对。
--
-- 本批新增两类取词位：
--
--   ① 业务错误（enums 常量值即 i18n key，不 seed 就会在页面上原样显示裸 key）：
--      · ErrStockQuantityRequired  —— 跟踪库存时必须显式填数量（0 是「卖光」，
--        必须由用户自己打出来，留空不等于 0）；
--      · ErrStockUntrackedQuantity —— 不跟踪（无限）的行不允许带数量：
--        数量与开关是同一件事的两种表达，同时存在就是自相矛盾的数据
--        （DDL 侧由 CHECK (track_quantity OR quantity = 0) 兜底，这里是可读的提前拦截）。
--
--   ② 库存页「该 SKU 的各仓库存」表的用户可见文案：
--      数量列的**三态**（∞ 无限 / 数字 / 未入库）与行内编辑「跟踪开关 + 数量」的表单。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrStockQuantityRequired', 'zh-CN', '跟踪库存时必须填写数量（0 表示已卖光，需要自己填出来，留空不等于 0）', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrStockQuantityRequired', 'en-US', 'A quantity is required when tracking stock (0 means sold out and must be entered explicitly; leaving it empty is not 0)', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrStockUntrackedQuantity', 'zh-CN', '不跟踪（无限）的库存行不允许带数量：要写具体数量请先切换成跟踪库存', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrStockUntrackedQuantity', 'en-US', 'Untracked (unlimited) stock rows cannot carry a quantity; switch to tracked stock first', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('admin.inventory.stock.unlimited', 'zh-CN', '无限', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.unlimited', 'en-US', 'Unlimited', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.notStocked', 'zh-CN', '未入库', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.notStocked', 'en-US', 'Not stocked', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.notStockedHint', 'zh-CN', '这个仓还没有这条 SKU 的库存行（未入库），先入库或调整才会出现', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.notStockedHint', 'en-US', 'This warehouse has no stock row for the SKU yet (not stocked); it appears after an inbound or an adjustment', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.track', 'zh-CN', '跟踪库存', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.track', 'en-US', 'Track stock', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.untracked', 'zh-CN', '不跟踪（无限）', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.untracked', 'en-US', 'Untracked (unlimited)', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.col.tracking', 'zh-CN', '跟踪与数量', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.col.tracking', 'en-US', 'Tracking & quantity', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.qtyPh', 'zh-CN', '数量（无限时留空）', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.qtyPh', 'en-US', 'Quantity (empty when unlimited)', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.qtyHint', 'zh-CN', '不跟踪时数量框留空且不可填；要写 0（卖光）必须自己打出来，页面不会预填 0', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.qtyHint', 'en-US', 'When untracked the quantity box stays empty and disabled; a 0 (sold out) must be typed by hand, the page never pre-fills 0', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.actionHint', 'zh-CN', '切换这条 SKU 在本仓的跟踪开关并写入数量；改成跟踪会按「手工调整」记一条流水', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.actionHint', 'en-US', 'Toggle tracking for this SKU in this warehouse and set the quantity; switching to tracked writes a manual-adjustment movement', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.save', 'zh-CN', '保存', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.stock.save', 'en-US', 'Save', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
