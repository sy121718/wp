-- 623b · W2（迁移 623）的文案词条：扣减优先级 + 采购单两个终态。
--
-- 本批新增两类取词位：
--
--   ① 业务错误（enums 常量值即 i18n key）：
--      · ErrPurchaseStatusNotFinishable —— pending 的单不能提前关单；
--      · ErrPurchaseStatusNotCancelable —— received 的单不能取消（货已进仓，要走退货）。
--
--   ② 界面文案：仓库页的「扣减优先级」列 / 表单 / 说明，采购单页的两个终态按钮、
--      确认文案与两个状态徽标。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.warehouse.col.deductPriority', 'zh-CN', '扣减优先级', 200, 'inventory', 'internal/templates/admin/inventory/inventory_warehouses.html', 1, now(), now()),
('admin.inventory.warehouse.col.deductPriority', 'en-US', 'Deduct priority', 200, 'inventory', 'internal/templates/admin/inventory/inventory_warehouses.html', 1, now(), now()),
('admin.inventory.warehouse.deductPriorityHint', 'zh-CN', '值小先扣；只在出库且没有指定仓库时生效，显式选仓永远优先。留空按 100。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_warehouses.html', 1, now(), now()),
('admin.inventory.warehouse.deductPriorityHint', 'en-US', 'Lower deducts first; only applies to outbound moves with no warehouse given — an explicit warehouse always wins. Empty means 100.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_warehouses.html', 1, now(), now()),
('admin.inventory_purchases.status.finished', 'zh-CN', '已关单', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.finished', 'en-US', 'Closed', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.cancelled', 'zh-CN', '已取消', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.cancelled', 'en-US', 'Cancelled', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.finish', 'zh-CN', '提前关单', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.finish', 'en-US', 'Close early', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.finishConfirm', 'zh-CN', '提前关单？剩下的不再等，单据进入已完成终态。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.finishConfirm', 'en-US', 'Close this purchase order early? The remaining quantity is no longer expected and the order becomes final.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.cancel', 'zh-CN', '取消单据', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.cancel', 'en-US', 'Cancel order', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.cancelConfirm', 'zh-CN', '取消这张采购单？已入库的货仍留在仓库里。', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('admin.inventory_purchases.cancelConfirm', 'en-US', 'Cancel this purchase order? Stock already received stays in the warehouse.', 200, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('ErrPurchaseStatusNotFinishable', 'zh-CN', '只有已入库或部分入库的采购单可以提前关单', 409, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('ErrPurchaseStatusNotFinishable', 'en-US', 'Only received or partially received purchase orders can be closed early', 409, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('ErrPurchaseStatusNotCancelable', 'zh-CN', '只有未入库或部分入库的采购单可以取消（已收满的货已进仓，要走退货）', 409, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now()),
('ErrPurchaseStatusNotCancelable', 'en-US', 'Only pending or partially received purchase orders can be cancelled; fully received stock already sits in the warehouse', 409, 'inventory', 'internal/templates/admin/inventory/inventory_purchases.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
