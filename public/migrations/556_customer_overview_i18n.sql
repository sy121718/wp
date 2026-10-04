-- 556 · 客户概览页的词条（admin/user/customer_overview.html）
--
-- 背景：客户目录（555）下的第一页 —— 区间的客户增长（新客 / 回头客 / 复购 / 复购率）。
--
-- 覆盖：20 个新 key × 2 语言 = 40 条（ON CONFLICT DO NOTHING）。
--
-- 时间档位那五条（admin.dashboard.range.today/yesterday/week/month/year）刻意**不重复 seed**：
-- 这一页与仪表盘共用同一批词条，各写一套的话迟早出现「两页的『本周』不是同一个意思」——
-- 而那是个没人会去核对的地方。
--
-- 页头「?」里那三段由四个 key 拼成（lead / lead.strong / lead.tail…），
-- 拆开是因为中间要夹 <strong>：整句塞进一个词条就没法加粗了。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customer.overview.title', 'en-US', 'Customer overview', 200, 'admin', 'admin/user/customer_overview.html: 页标题', 1, now(), now()),
('admin.customer.overview.title', 'zh-CN', '客户概览', 200, 'admin', 'admin/user/customer_overview.html: 页标题', 1, now(), now()),
('admin.customer.overview.help.label', 'en-US', 'View help', 200, 'admin', 'admin/user/customer_overview.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.customer.overview.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/user/customer_overview.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.customer.overview.hint.lead', 'en-US', 'Here a "new" customer means one whose ', 200, 'admin', 'admin/user/customer_overview.html: 说明（新客口径前段）', 1, now(), now()),
('admin.customer.overview.hint.lead', 'zh-CN', '这里的「新客」按', 200, 'admin', 'admin/user/customer_overview.html: 说明（新客口径前段）', 1, now(), now()),
('admin.customer.overview.hint.lead.strong', 'en-US', 'first order', 200, 'admin', 'admin/user/customer_overview.html: 说明（新客口径加粗词）', 1, now(), now()),
('admin.customer.overview.hint.lead.strong', 'zh-CN', '首次下单', 200, 'admin', 'admin/user/customer_overview.html: 说明（新客口径加粗词）', 1, now(), now()),
('admin.customer.overview.hint.lead.tail', 'en-US', ' falls in this range — not by signup date. Someone who registered months ago and ordered for the first time now is still new for this range.', 200, 'admin', 'admin/user/customer_overview.html: 说明（新客口径后段）', 1, now(), now()),
('admin.customer.overview.hint.lead.tail', 'zh-CN', '算，不按注册时间 —— 有人注册了半年才下第一单，他对这段时间来说是新人。', 200, 'admin', 'admin/user/customer_overview.html: 说明（新客口径后段）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.strong', 'en-US', 'Repurchase', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径加粗词）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.strong', 'zh-CN', '复购', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径加粗词）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.mid', 'en-US', ' counts people who placed two or more orders ', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径中段）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.mid', 'zh-CN', '指的是', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径中段）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.strong2', 'en-US', 'within this range', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径范围加粗词）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.strong2', 'zh-CN', '在这段时间内', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径范围加粗词）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.tail', 'en-US', ' — one order last month and one this month does not count as a repurchase here. Paid-and-later orders only; guest orders (no account) are excluded.', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径后段）', 1, now(), now()),
('admin.customer.overview.hint.repurchase.tail', 'zh-CN', '下了两单及以上的人 —— 上个月和这个月各下一单不算在这段时间的复购里。只统计已付款及以后的订单，游客单（没有账号）不计入。', 200, 'admin', 'admin/user/customer_overview.html: 说明（复购口径后段）', 1, now(), now()),
('admin.customer.overview.empty', 'en-US', 'No customers to measure in this range: either no site project exists yet, or there are no paid orders. Try another range.', 200, 'admin', 'admin/user/customer_overview.html: 无数据空态', 1, now(), now()),
('admin.customer.overview.empty', 'zh-CN', '这段时间里没有可统计的客户：可能是还没有站点工程，也可能确实没有已付款的订单。换个时间段试试。', 200, 'admin', 'admin/user/customer_overview.html: 无数据空态', 1, now(), now()),
('admin.customer.overview.ordering', 'en-US', 'Ordering customers', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（区间下单客户）', 1, now(), now()),
('admin.customer.overview.ordering', 'zh-CN', '区间下单客户', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（区间下单客户）', 1, now(), now()),
('admin.customer.overview.orderingNote', 'en-US', 'People with a paid order in this range', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（区间下单客户）', 1, now(), now()),
('admin.customer.overview.orderingNote', 'zh-CN', '这段时间里有已付款订单的人数', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（区间下单客户）', 1, now(), now()),
('admin.customer.overview.new', 'en-US', 'New customers', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（新客）', 1, now(), now()),
('admin.customer.overview.new', 'zh-CN', '新客', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（新客）', 1, now(), now()),
('admin.customer.overview.newNote', 'en-US', 'First order falls in this range', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（新客）', 1, now(), now()),
('admin.customer.overview.newNote', 'zh-CN', '首次下单落在这段时间', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（新客）', 1, now(), now()),
('admin.customer.overview.returning', 'en-US', 'Returning customers', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（回头客）', 1, now(), now()),
('admin.customer.overview.returning', 'zh-CN', '回头客', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（回头客）', 1, now(), now()),
('admin.customer.overview.returningNote', 'en-US', 'First order before this range', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（回头客）', 1, now(), now()),
('admin.customer.overview.returningNote', 'zh-CN', '首单在这段时间之前', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（回头客）', 1, now(), now()),
('admin.customer.overview.repurchasers', 'en-US', 'Repeat buyers', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（复购客户）', 1, now(), now()),
('admin.customer.overview.repurchasers', 'zh-CN', '复购客户', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（复购客户）', 1, now(), now()),
('admin.customer.overview.repurchasersNote', 'en-US', 'Of which new: ', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（复购中的新客）', 1, now(), now()),
('admin.customer.overview.repurchasersNote', 'zh-CN', '其中新客：', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（复购中的新客）', 1, now(), now()),
('admin.customer.overview.repurchaseRate', 'en-US', 'Repurchase rate', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（复购率）', 1, now(), now()),
('admin.customer.overview.repurchaseRate', 'zh-CN', '复购率', 200, 'admin', 'admin/user/customer_overview.html: KPI 标签（复购率）', 1, now(), now()),
('admin.customer.overview.repurchaseRateNote', 'en-US', 'Repeat buyers / ordering customers', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（复购率）', 1, now(), now()),
('admin.customer.overview.repurchaseRateNote', 'zh-CN', '复购人数 ÷ 区间下单客户', 200, 'admin', 'admin/user/customer_overview.html: KPI 注释（复购率）', 1, now(), now());
