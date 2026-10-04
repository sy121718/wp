-- 558 · 客户列表的「消费分段」筛选（admin/user/customers.html）
--
-- 背景：客户列表可以按「新客 / 回头客 / 复购」筛。这三条口径在订单模块
--       （见 order_customer_segment_model.go），本页只是把结果当过滤条件用。
--
-- 覆盖：8 个新 key × 2 语言 = 16 条（ON CONFLICT DO NOTHING）。
--
-- 「全部客户」是必选项而不是留空：没有它，用户无法把已选的分段取消掉
--（只能点「重置」，那会连关键词一起清掉）。
--
-- 两个日期的措辞用「下单时间」而不是「时间」：本页还有一组「注册时间」，
-- 两组日期很靠近，标签含糊时用户会把它们当成同一件事 —— 而它们筛的是两批人。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customers.field.segment', 'en-US', 'Purchase segment', 200, 'admin', 'admin/user/customers.html: 筛选标签（消费分段）', 1, now(), now()),
('admin.customers.field.segment', 'zh-CN', '消费分段', 200, 'admin', 'admin/user/customers.html: 筛选标签（消费分段）', 1, now(), now()),
('admin.customers.segment.all', 'en-US', 'All customers', 200, 'admin', 'admin/user/customers.html: 分段选项（全部）', 1, now(), now()),
('admin.customers.segment.all', 'zh-CN', '全部客户', 200, 'admin', 'admin/user/customers.html: 分段选项（全部）', 1, now(), now()),
('admin.customers.segment.new', 'en-US', 'New (first order in range)', 200, 'admin', 'admin/user/customers.html: 分段选项（新客）', 1, now(), now()),
('admin.customers.segment.new', 'zh-CN', '新客（首次下单在这段时间）', 200, 'admin', 'admin/user/customers.html: 分段选项（新客）', 1, now(), now()),
('admin.customers.segment.returning', 'en-US', 'Returning (first order earlier)', 200, 'admin', 'admin/user/customers.html: 分段选项（回头客）', 1, now(), now()),
('admin.customers.segment.returning', 'zh-CN', '回头客（首单在这段时间之前）', 200, 'admin', 'admin/user/customers.html: 分段选项（回头客）', 1, now(), now()),
('admin.customers.segment.repurchasing', 'en-US', 'Repeat (2+ orders in range)', 200, 'admin', 'admin/user/customers.html: 分段选项（复购）', 1, now(), now()),
('admin.customers.segment.repurchasing', 'zh-CN', '复购（这段时间内 ≥2 单）', 200, 'admin', 'admin/user/customers.html: 分段选项（复购）', 1, now(), now()),
('admin.customers.segment.note', 'en-US', 'Defaults to this month; paid-and-later orders only, guest orders excluded.', 200, 'admin', 'admin/user/customers.html: 分段筛选说明', 1, now(), now()),
('admin.customers.segment.note', 'zh-CN', '不选时间就是本月；只统计已付款及以后的订单，游客单不计入。', 200, 'admin', 'admin/user/customers.html: 分段筛选说明', 1, now(), now()),
('admin.customers.field.segment_from', 'en-US', 'Ordered from', 200, 'admin', 'admin/user/customers.html: 分段窗口起点', 1, now(), now()),
('admin.customers.field.segment_from', 'zh-CN', '下单时间（起）', 200, 'admin', 'admin/user/customers.html: 分段窗口起点', 1, now(), now()),
('admin.customers.field.segment_to', 'en-US', 'Ordered to', 200, 'admin', 'admin/user/customers.html: 分段窗口终点', 1, now(), now()),
('admin.customers.field.segment_to', 'zh-CN', '下单时间（止）', 200, 'admin', 'admin/user/customers.html: 分段窗口终点', 1, now(), now());
