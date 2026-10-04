-- 563 · 客户列表「复购次数」筛选的词条（admin/user/customers.html）
--
-- 背景：客户列表已有消费分段（新客 / 回头客 / 复购），本批补上复购**次数**档位。
--       它与「复购（≥2 单）」走同一段订单侧代码，只是门槛可抬高 ——
--       单独开一个分段名会让「什么算复购」出现第二个定义。
--
-- 覆盖：7 个新 key × 2 语言 = 14 条。
--
-- 档位只给 2 / 3 / 5 / 10：收任意值的话「≥7 次」会出现在 URL 里并被分享回放，
-- 而运营真正需要的档位只有这几个。不在这张表里的一律回落「不限次数」
--（回落成默认门槛 2 更糟：URL 上写着 7、实际跑的是 2，看起来正常却少了一半人）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customers.field.min_orders', 'en-US', 'Times ordered', 200, 'admin', 'admin/user/customers.html: 筛选标签（复购次数）', 1, now(), now()),
('admin.customers.field.min_orders', 'zh-CN', '复购次数', 200, 'admin', 'admin/user/customers.html: 筛选标签（复购次数）', 1, now(), now()),
('admin.customers.minOrders.any', 'en-US', 'Any', 200, 'admin', 'admin/user/customers.html: 次数选项（不限）', 1, now(), now()),
('admin.customers.minOrders.any', 'zh-CN', '不限次数', 200, 'admin', 'admin/user/customers.html: 次数选项（不限）', 1, now(), now()),
('admin.customers.minOrders.2', 'en-US', 'Ordered 2+ times', 200, 'admin', 'admin/user/customers.html: 次数选项（≥2）', 1, now(), now()),
('admin.customers.minOrders.2', 'zh-CN', '下过 ≥2 单', 200, 'admin', 'admin/user/customers.html: 次数选项（≥2）', 1, now(), now()),
('admin.customers.minOrders.3', 'en-US', 'Ordered 3+ times', 200, 'admin', 'admin/user/customers.html: 次数选项（≥3）', 1, now(), now()),
('admin.customers.minOrders.3', 'zh-CN', '下过 ≥3 单', 200, 'admin', 'admin/user/customers.html: 次数选项（≥3）', 1, now(), now()),
('admin.customers.minOrders.5', 'en-US', 'Ordered 5+ times', 200, 'admin', 'admin/user/customers.html: 次数选项（≥5）', 1, now(), now()),
('admin.customers.minOrders.5', 'zh-CN', '下过 ≥5 单', 200, 'admin', 'admin/user/customers.html: 次数选项（≥5）', 1, now(), now()),
('admin.customers.minOrders.10', 'en-US', 'Ordered 10+ times', 200, 'admin', 'admin/user/customers.html: 次数选项（≥10）', 1, now(), now()),
('admin.customers.minOrders.10', 'zh-CN', '下过 ≥10 单', 200, 'admin', 'admin/user/customers.html: 次数选项（≥10）', 1, now(), now()),
('admin.customers.minOrders.note', 'en-US', 'Order counts only cover the selected window; with no dates it is this month.', 200, 'admin', 'admin/user/customers.html: 次数筛选说明', 1, now(), now()),
('admin.customers.minOrders.note', 'zh-CN', '下单次数只算所选时间段内的订单；不选时间就是本月。', 200, 'admin', 'admin/user/customers.html: 次数筛选说明', 1, now(), now());
