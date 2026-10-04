-- 552 · 概览页图表双 Tab 的词条（admin/dashboard.html）
--
-- 背景：图表拆成两个 Tab —— 「销售额与订单」与「页面浏览」。两张图共用一根日期轴，
--       各画各的柱子（服务端给两套柱高），切换是纯前端行为。
--
-- 覆盖：5 个新 key × 2 语言 = 10 条（ON CONFLICT DO NOTHING），
--       另加 3 个既有 key × 2 语言的文案覆盖（UPDATE，WHERE 值不同，重复执行不动行）。
--
-- 为什么 UPDATE 既有 key：标题从「销售趋势（…）」改成「趋势（…）」—— 加了 Tab 之后
-- 那张卡片不再只画销售额，标题里再写「销售趋势」会与第二个 Tab 的「页面浏览」矛盾。
-- 空态文案同理：不再只是「没有订单」，而是两侧都没有数据。
-- 模板的取值链是 t(key, fallback)，fallback 只在**词条缺失**时生效，所以必须 UPDATE
-- 而不是重插（迁移 316 / 540 / 550 各记过一次这个坑）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.chart.tabs', 'en-US', 'Chart metric', 200, 'admin', 'admin/dashboard.html: 图表 Tab 组无障碍标签', 1, now(), now()),
('admin.dashboard.chart.tabs', 'zh-CN', '图表维度', 200, 'admin', 'admin/dashboard.html: 图表 Tab 组无障碍标签', 1, now(), now()),
('admin.dashboard.chart.tabSales', 'en-US', 'Sales and orders', 200, 'admin', 'admin/dashboard.html: 图表 Tab：销售额与订单', 1, now(), now()),
('admin.dashboard.chart.tabSales', 'zh-CN', '销售额与订单', 200, 'admin', 'admin/dashboard.html: 图表 Tab：销售额与订单', 1, now(), now()),
('admin.dashboard.chart.tabViews', 'en-US', 'Page views', 200, 'admin', 'admin/dashboard.html: 图表 Tab：页面浏览', 1, now(), now()),
('admin.dashboard.chart.tabViews', 'zh-CN', '页面浏览', 200, 'admin', 'admin/dashboard.html: 图表 Tab：页面浏览', 1, now(), now()),
('admin.dashboard.chart.viewsUnit', 'en-US', 'views', 200, 'admin', 'admin/dashboard.html: 浏览量柱子提示里的单位', 1, now(), now()),
('admin.dashboard.chart.viewsUnit', 'zh-CN', '浏览', 200, 'admin', 'admin/dashboard.html: 浏览量柱子提示里的单位', 1, now(), now()),
('admin.dashboard.chart.viewsAria', 'en-US', 'Page views bar chart', 200, 'admin', 'admin/dashboard.html: 浏览量 SVG 的无障碍标签', 1, now(), now()),
('admin.dashboard.chart.viewsAria', 'zh-CN', '页面浏览柱状图', 200, 'admin', 'admin/dashboard.html: 浏览量 SVG 的无障碍标签', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 既有 key 的文案覆盖：标题与空态不再只讲「销售」。
UPDATE sys_i18n SET item_value = 'Trend (daily)', update_time = now()
WHERE item_key = 'admin.dashboard.trend.title' AND lang = 'en-US' AND item_value <> 'Trend (daily)';
UPDATE sys_i18n SET item_value = '趋势（按天）', update_time = now()
WHERE item_key = 'admin.dashboard.trend.title' AND lang = 'zh-CN' AND item_value <> '趋势（按天）';
UPDATE sys_i18n SET item_value = 'Trend (weekly)', update_time = now()
WHERE item_key = 'admin.dashboard.trend.titleWeekly' AND lang = 'en-US' AND item_value <> 'Trend (weekly)';
UPDATE sys_i18n SET item_value = '趋势（按周）', update_time = now()
WHERE item_key = 'admin.dashboard.trend.titleWeekly' AND lang = 'zh-CN' AND item_value <> '趋势（按周）';
UPDATE sys_i18n SET item_value = 'No data in this range', update_time = now()
WHERE item_key = 'admin.dashboard.trend.empty' AND lang = 'en-US' AND item_value <> 'No data in this range';
UPDATE sys_i18n SET item_value = '这段时间没有数据', update_time = now()
WHERE item_key = 'admin.dashboard.trend.empty' AND lang = 'zh-CN' AND item_value <> '这段时间没有数据';
