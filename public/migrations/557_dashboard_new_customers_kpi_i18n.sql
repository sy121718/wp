-- 557 · 概览页「新客户」KPI 卡（admin/dashboard.html）
--
-- 背景：概览页补上第 6 张卡 —— 区间内的新客数。数字来自 P7-a 的区间客户增长聚合，
--       与客户概览页调的是同一个方法、传的是同一个区间，所以两处显示的必须是同一个数。
--
-- 覆盖：2 个新 key × 2 语言 = 4 条（ON CONFLICT DO NOTHING）。
--
-- 为什么不复用客户概览页的 admin.customer.overview.new：那两条词条与这一页的
-- 上下文不同（那一页挤在四五个客户指标之间，这一页夹在订单与浏览量之间），
-- 措辞会各自演化。共享一个 key 的话，改一处会连带改另一处而没人会发现 ——
-- 而两页「新客」的**数字**是同源的（同一个方法），文案却不必绑死。
-- （与时间档位那批相反：admin.dashboard.range.* 必须复用，因为那批是同一批档位，
--   两页的「本周」不同义才是真问题。）

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.kpi.newCustomers', 'en-US', 'New customers', 200, 'admin', 'admin/dashboard.html: KPI 标签（区间新客）', 1, now(), now()),
('admin.dashboard.kpi.newCustomers', 'zh-CN', '新客户', 200, 'admin', 'admin/dashboard.html: KPI 标签（区间新客）', 1, now(), now()),
('admin.dashboard.kpi.newCustomersNote', 'en-US', 'First order falls in this range', 200, 'admin', 'admin/dashboard.html: KPI 注释（区间新客）', 1, now(), now()),
('admin.dashboard.kpi.newCustomersNote', 'zh-CN', '首次下单落在这段时间', 200, 'admin', 'admin/dashboard.html: KPI 注释（区间新客）', 1, now(), now());
