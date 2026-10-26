-- 647 · 库存流水独立页（B4）的界面词条。
--
-- 流水从 /admin/inventory 拆成独立页后，模板 internal/templates/admin/inventory/inventory_movements.html
-- 引入了一批新 key；旧 key（admin.inventory.moves.* 与 admin.inventory.change.*）沿用不动 ——
-- 库存管理页仍在用同一批，且它们已经 seed 过（迁移 191 / 625 那两批）。
--
-- 只 seed 这里真正新增的 12 个 key（中英成对共 24 行）：
-- 已 seed 的 key 再写一遍会让「未 seed」门禁看不出「本批到底补了什么」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.movements.title', 'zh-CN', '库存流水', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.title', 'en-US', 'Stock movements', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.lead', 'zh-CN', '每一次库存真源变动都留痕：方向、数量、变动前后值、原因、来源引用与批次号。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.lead', 'en-US', 'Every change to the stock of record leaves a trace: direction, quantity, before/after values, reason, source reference and batch id.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.snapshot', 'zh-CN', '货品名、图片与单价是变动当时的快照 —— 之后改主档不改写历史。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.snapshot', 'en-US', 'The item name, image and unit cost are snapshots taken at the moment of the change - later edits to the master record do not rewrite history.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.timeHint', 'zh-CN', '时间筛选按流水创建时间过滤；只填日期时，截止日按当天 23:59:59 收口。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.timeHint', 'en-US', 'The time filter uses the movement creation time; when only a date is given, the end date closes at 23:59:59 of that day.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.boundary', 'zh-CN', '本页只读。入库 / 出库请走单据（采购入库、发货、退货、调拨），盘点与报损在', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.boundary', 'en-US', 'This page is read-only. Use the documents for stock in / out (purchase receipt, shipment, return, transfer); stocktake and write-off live on the ', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.boundaryLink', 'zh-CN', '库存管理', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.boundaryLink', 'en-US', 'inventory', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.boundaryTail', 'zh-CN', '页的调整抽屉里。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.boundaryTail', 'en-US', ' page, in the adjustment drawer.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.listLead', 'zh-CN', '库存流水（本页 ', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.listLead', 'en-US', 'Stock movements (this page: ', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.col.item', 'zh-CN', '货品', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.col.item', 'en-US', 'Item', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.col.source', 'zh-CN', '来源 / 操作人', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.col.source', 'en-US', 'Source / operator', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.col.unitCost', 'zh-CN', '单价', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.col.unitCost', 'en-US', 'Unit cost', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.emptyDesc', 'zh-CN', '库存还没有变动过，或者当前筛选条件把记录都过滤掉了。入库请走采购入库，盘点 / 报损在库存管理页。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now()),
('admin.inventory.movements.emptyDesc', 'en-US', 'Stock has not changed yet, or the current filters excluded every record. Use purchase receipt for stock in; stocktake and write-off live on the inventory page.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_movements.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
