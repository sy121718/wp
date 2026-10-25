-- 630b · W5 折算块的文案词条（迁移 633 的页面部分）。
--
-- 「汇率未固化」是**必须显示**的：折算金额只覆盖有快照的那部分，
-- 不说明「有多少单没折算」会让一个偏小的数字被当成全量。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.order.sales.converted.title', 'zh-CN', '折算到', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.title', 'en-US', 'Converted to', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.hint', 'zh-CN', '按下单时刻的汇率快照折算；汇率之后再变不影响这些数字。', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.hint', 'en-US', 'Converted with the rate snapshot taken at order time; later rate changes do not affect these numbers.', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.sales', 'zh-CN', '折算销售额', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.sales', 'en-US', 'Converted sales', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.profit', 'zh-CN', '折算毛利', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.profit', 'en-US', 'Converted gross profit', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.orders', 'zh-CN', '已折算订单', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.orders', 'en-US', 'Converted orders', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.unconverted', 'zh-CN', '汇率未固化', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.unconverted', 'en-US', 'Rate not captured', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.unconvertedNote', 'zh-CN', '这些单没有汇率快照，不进上面的折算金额', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now()),
('admin.order.sales.converted.unconvertedNote', 'en-US', 'These orders have no rate snapshot and are excluded from the converted amounts', 200, 'admin', 'internal/templates/admin/order/sales_overview.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
