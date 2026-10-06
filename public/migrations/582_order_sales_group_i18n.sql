-- 582 · 销售概览页的分组标题与趋势空态词条（BIZ-1）。
--
-- 视觉改造带进来的 3 个 key：
--   group.sales / group.customers  两排卡片的小标题（八张同形卡没有分组标识时
--                                  读不出「前四张是钱、后四张是人」）
--   monthly.empty                  回看窗口里一笔销售都没有时的空态文案
--                                  （此前的形态是渲染一块 160px 高的空白画布，
--                                   那会被读成「趋势平缓」而不是「没数据」）
--
-- 「?」按钮的 aria-label 用的是 579 那一批已有的 admin.common.help.label，不重复种。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.order.sales.group.sales', 'zh-CN', '销售'),
('admin.order.sales.group.sales', 'en-US', 'Sales'),
('admin.order.sales.group.customers', 'zh-CN', '客户'),
('admin.order.sales.group.customers', 'en-US', 'Customers'),
('admin.order.sales.monthly.empty', 'zh-CN', '这段时间没有计入消费的订单，趋势暂无可画的柱子。'),
('admin.order.sales.monthly.empty', 'en-US', 'No paid orders in this window, so there is nothing to plot yet.')
ON CONFLICT (item_key, lang) DO NOTHING;
