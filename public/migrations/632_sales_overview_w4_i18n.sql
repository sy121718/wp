-- 629 · W4（迁移 633 / 623 + 销售总览改造）的文案词条。
--
-- 两类取词位：
--   ① 销售总览四卡（总订单数 / 总订购件数 / 销售总额 / 毛利）：两个新卡 + 毛利的
--      三条说明（含「成本未核算」那条必须显示 —— 把未知成本记成 0 会让毛利虚高）；
--   ② 库存预警四卡与「共 N 项待办」块。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.order.sales.label.units', 'zh-CN', '总订购件数', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.label.units', 'en-US', 'Units ordered', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.note.units', 'zh-CN', 'Σ 明细数量（一单多行各算一次）', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.note.units', 'en-US', 'Σ item quantity (each line counts once)', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.costIncomplete', 'zh-CN', '含', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.costIncomplete', 'en-US', 'Includes', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.costIncompleteTail', 'zh-CN', '行成本未核算', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.costIncompleteTail', 'en-US', 'lines without cost', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.titleLead', 'zh-CN', '共', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.titleLead', 'en-US', 'Items:', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.titleTail', 'zh-CN', '项待办', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.titleTail', 'en-US', 'need attention', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.label.grossProfit', 'zh-CN', '毛利', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.label.grossProfit', 'en-US', 'Gross profit', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.note.grossProfit', 'zh-CN', '毛利 = 行实付 − 行成本 − 运费；税不扣（代收）、优惠已含在行实付里不重复扣。', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.note.grossProfit', 'en-US', 'Gross profit = line amounts paid − line cost − shipping. Tax is not deducted (collected on behalf), discounts are already inside the line amounts.', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('inventory.stock.alert.title', 'zh-CN', '库存预警', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.title', 'en-US', 'Stock alerts', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.total', 'zh-CN', '跟踪数量的库存行', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.total', 'en-US', 'Tracked stock rows', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.outOfStock', 'zh-CN', '缺货', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.outOfStock', 'en-US', 'Out of stock', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.lowStock', 'zh-CN', '可售为 0', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.lowStock', 'en-US', 'No available stock', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.alert', 'zh-CN', '要处理', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.alert', 'en-US', 'Needs action', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.hint', 'zh-CN', '只统计跟踪数量的行（无限库存永远「有货」，不进预警）。「可售为 0」= 货在仓里但全被未发货订单占住。', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('inventory.stock.alert.hint', 'en-US', 'Only tracked rows are counted (unlimited stock always has goods and never alerts). "No available stock" means goods are in the warehouse but every unit is held by unshipped orders.', 200, 'admin', 'internal/templates/admin/inventory/inventory_skus.html', 1, now(), now()),
('admin.order.sales.todo.transfers', 'zh-CN', '调拨待出货', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.transfers', 'en-US', 'Transfers pending shipment', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.alerts', 'zh-CN', '库存预警', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.alerts', 'en-US', 'Stock alerts', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.pendingShip', 'zh-CN', '待发货', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.pendingShip', 'en-US', 'Orders awaiting shipment', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.empty', 'zh-CN', '没有待办项', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.todo.empty', 'en-US', 'Nothing to do', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
