-- 550 · 概览页时间筛选条与区间口径的词条（admin/dashboard.html）
--
-- 背景：概览页从「固定今日 / 近 7 天」升级为「可切区间」——新增时间筛选条
--       （今日/昨日/本周/本月/本年/自定义），KPI 与两张卡的标题随之从「今日」「近 7 天」
--       改为区间口径。
--
-- 覆盖：15 个新 key × 2 语言 = 30 条（ON CONFLICT DO NOTHING），
--       另加 2 个既有 key × 2 语言的文案覆盖（UPDATE，WHERE 值不同，重复执行不动行）。
--
-- 为什么要 UPDATE 而不是重插：模板的取值链是 t(key, fallback)，fallback 只在**词条缺失**
-- 时生效。trend.title / top.title 早已 seed 成「近 7 天」口径，再插同 key 会被 DO NOTHING
-- 静默跳过 —— 页面上写着「按天」、库里还是「近 7 天」（迁移 316 与 540 各记过一次）。
--
-- 注意 kpi.ordersToday / kpi.salesToday 两个旧 key **刻意留着不动**：
-- 540 的注册判据拿 salesToday 当代表 key（迁移器据此判断 540 是否已完成）。
-- 删掉它会让 540 在每次启动时被判为「未完成」而重跑，把行插回来 —— 删了等于没删。
-- 它们已无任何引用点（rg 可查），留着只是两条不生效的历史词条。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.kpi.orders', 'en-US', 'Orders', 200, 'admin', 'admin/dashboard.html: 区间订单数 KPI', 1, now(), now()),
('admin.dashboard.kpi.orders', 'zh-CN', '订单数', 200, 'admin', 'admin/dashboard.html: 区间订单数 KPI', 1, now(), now()),
('admin.dashboard.kpi.ordersNote', 'en-US', 'All orders created in this range (cancellations included)', 200, 'admin', 'admin/dashboard.html: 订单数 KPI 的口径说明', 1, now(), now()),
('admin.dashboard.kpi.ordersNote', 'zh-CN', '区间内创建的全部订单（含取消）', 200, 'admin', 'admin/dashboard.html: 订单数 KPI 的口径说明', 1, now(), now()),
('admin.dashboard.kpi.sales', 'en-US', 'Net sales', 200, 'admin', 'admin/dashboard.html: 区间净销售额 KPI', 1, now(), now()),
('admin.dashboard.kpi.sales', 'zh-CN', '净销售额', 200, 'admin', 'admin/dashboard.html: 区间净销售额 KPI', 1, now(), now()),

('admin.dashboard.range.today', 'en-US', 'Today', 200, 'admin', 'admin/dashboard.html: 时间筛选条：今日', 1, now(), now()),
('admin.dashboard.range.today', 'zh-CN', '今日', 200, 'admin', 'admin/dashboard.html: 时间筛选条：今日', 1, now(), now()),
('admin.dashboard.range.yesterday', 'en-US', 'Yesterday', 200, 'admin', 'admin/dashboard.html: 时间筛选条：昨日', 1, now(), now()),
('admin.dashboard.range.yesterday', 'zh-CN', '昨日', 200, 'admin', 'admin/dashboard.html: 时间筛选条：昨日', 1, now(), now()),
('admin.dashboard.range.week', 'en-US', 'This week', 200, 'admin', 'admin/dashboard.html: 时间筛选条：本周（ISO 周，周一起）', 1, now(), now()),
('admin.dashboard.range.week', 'zh-CN', '本周', 200, 'admin', 'admin/dashboard.html: 时间筛选条：本周（ISO 周，周一起）', 1, now(), now()),
('admin.dashboard.range.month', 'en-US', 'This month', 200, 'admin', 'admin/dashboard.html: 时间筛选条：本月（1 号起）', 1, now(), now()),
('admin.dashboard.range.month', 'zh-CN', '本月', 200, 'admin', 'admin/dashboard.html: 时间筛选条：本月（1 号起）', 1, now(), now()),
('admin.dashboard.range.year', 'en-US', 'This year', 200, 'admin', 'admin/dashboard.html: 时间筛选条：本年（1 月 1 日起）', 1, now(), now()),
('admin.dashboard.range.year', 'zh-CN', '本年', 200, 'admin', 'admin/dashboard.html: 时间筛选条：本年（1 月 1 日起）', 1, now(), now()),
('admin.dashboard.range.custom', 'en-US', 'Custom', 200, 'admin', 'admin/dashboard.html: 时间筛选条：自定义区间', 1, now(), now()),
('admin.dashboard.range.custom', 'zh-CN', '自定义', 200, 'admin', 'admin/dashboard.html: 时间筛选条：自定义区间', 1, now(), now()),
('admin.dashboard.range.from', 'en-US', 'Start date', 200, 'admin', 'admin/dashboard.html: 起止日期框的无障碍标签', 1, now(), now()),
('admin.dashboard.range.from', 'zh-CN', '起始日', 200, 'admin', 'admin/dashboard.html: 起止日期框的无障碍标签', 1, now(), now()),
('admin.dashboard.range.to', 'en-US', 'End date', 200, 'admin', 'admin/dashboard.html: 起止日期框的无障碍标签', 1, now(), now()),
('admin.dashboard.range.to', 'zh-CN', '结束日', 200, 'admin', 'admin/dashboard.html: 起止日期框的无障碍标签', 1, now(), now()),
('admin.dashboard.range.apply', 'en-US', 'Apply', 200, 'admin', 'admin/dashboard.html: 自定义区间的提交按钮', 1, now(), now()),
('admin.dashboard.range.apply', 'zh-CN', '应用', 200, 'admin', 'admin/dashboard.html: 自定义区间的提交按钮', 1, now(), now()),
('admin.dashboard.range.clamped', 'en-US', ' (range clipped to the maximum length)', 200, 'admin', 'admin/dashboard.html: 区间被收敛时的提示（拼在日期串后面）', 1, now(), now()),
('admin.dashboard.range.clamped', 'zh-CN', '（区间已按上限截取）', 200, 'admin', 'admin/dashboard.html: 区间被收敛时的提示（拼在日期串后面）', 1, now(), now()),

('admin.dashboard.trend.titleWeekly', 'en-US', 'Sales trend (weekly)', 200, 'admin', 'admin/dashboard.html: 趋势块标题（按周聚合）', 1, now(), now()),
('admin.dashboard.trend.titleWeekly', 'zh-CN', '销售趋势（按周）', 200, 'admin', 'admin/dashboard.html: 趋势块标题（按周聚合）', 1, now(), now()),
('admin.dashboard.trend.aria', 'en-US', 'Sales trend bar chart', 200, 'admin', 'admin/dashboard.html: 趋势 SVG 的无障碍标签（不含粒度，粒度在标题里）', 1, now(), now()),
('admin.dashboard.trend.aria', 'zh-CN', '销售趋势柱状图', 200, 'admin', 'admin/dashboard.html: 趋势 SVG 的无障碍标签（不含粒度，粒度在标题里）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 既有 key 的文案覆盖：从「今日 / 近 7 天」改成区间口径。
UPDATE sys_i18n SET item_value = 'Sales trend (daily)', update_time = now()
WHERE item_key = 'admin.dashboard.trend.title' AND lang = 'en-US' AND item_value <> 'Sales trend (daily)';
UPDATE sys_i18n SET item_value = '销售趋势（按天）', update_time = now()
WHERE item_key = 'admin.dashboard.trend.title' AND lang = 'zh-CN' AND item_value <> '销售趋势（按天）';
UPDATE sys_i18n SET item_value = 'Top products (current range)', update_time = now()
WHERE item_key = 'admin.dashboard.top.title' AND lang = 'en-US' AND item_value <> 'Top products (current range)';
UPDATE sys_i18n SET item_value = '热销商品（当前区间）', update_time = now()
WHERE item_key = 'admin.dashboard.top.title' AND lang = 'zh-CN' AND item_value <> '热销商品（当前区间）';
