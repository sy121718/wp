-- 540 · i18n 词条 seed（概览页：KPI 四卡 / 销售趋势 / 热销商品 / 站点内容小节）
--
-- 背景：仪表盘从「工程与页面统计」升级为「经营概览」——KPI 换成今日订单、今日销售额、
--       文章浏览量、待发货，并新增近 7 天销售趋势与热销商品榜。
-- 覆盖：19 个新 key × 2 语言 + 1 个既有 key 的口径说明覆盖（admin.dashboard.intro）。
-- 幂等：新 key 走 ON CONFLICT DO NOTHING；intro 走 UPDATE（WHERE 值不同），重复执行不动行。
--
-- intro 为什么必须 UPDATE：模板的取值链是 t(key, fallback)，fallback 只在**词条缺失**时生效，
-- 而 admin.dashboard.intro 早已 seed 成旧的「工程数 / 页面数」口径。再插一次同 key 会被
-- DO NOTHING 静默跳过 —— 页面上写着新口径、库里还是旧文案（迁移 316 记过这个坑）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.kpi.ordersToday', 'en-US', 'Orders today', 200, 'admin', 'admin/dashboard.html: 今日订单 KPI', 1, now(), now()),
('admin.dashboard.kpi.ordersToday', 'zh-CN', '今日订单', 200, 'admin', 'admin/dashboard.html: 今日订单 KPI', 1, now(), now()),
('admin.dashboard.kpi.salesToday', 'en-US', 'Sales today', 200, 'admin', 'admin/dashboard.html: 今日净销售额 KPI', 1, now(), now()),
('admin.dashboard.kpi.salesToday', 'zh-CN', '今日销售额', 200, 'admin', 'admin/dashboard.html: 今日净销售额 KPI', 1, now(), now()),
('admin.dashboard.kpi.articleViews', 'en-US', 'Article views', 200, 'admin', 'admin/dashboard.html: 今日文章页浏览量 KPI', 1, now(), now()),
('admin.dashboard.kpi.articleViews', 'zh-CN', '文章浏览', 200, 'admin', 'admin/dashboard.html: 今日文章页浏览量 KPI', 1, now(), now()),
('admin.dashboard.kpi.shipPending', 'en-US', 'To ship', 200, 'admin', 'admin/dashboard.html: 待发货 KPI', 1, now(), now()),
('admin.dashboard.kpi.shipPending', 'zh-CN', '待发货', 200, 'admin', 'admin/dashboard.html: 待发货 KPI', 1, now(), now()),
('admin.dashboard.kpi.pendingNote', 'en-US', 'Awaiting payment: ', 200, 'admin', 'admin/dashboard.html: 待发货卡片下的待付款小注', 1, now(), now()),
('admin.dashboard.kpi.pendingNote', 'zh-CN', '待付款：', 200, 'admin', 'admin/dashboard.html: 待发货卡片下的待付款小注', 1, now(), now()),
('admin.dashboard.trend.title', 'en-US', 'Sales trend (last 7 days)', 200, 'admin', 'admin/dashboard.html: 销售趋势块标题', 1, now(), now()),
('admin.dashboard.trend.title', 'zh-CN', '销售趋势（近 7 天）', 200, 'admin', 'admin/dashboard.html: 销售趋势块标题', 1, now(), now()),
('admin.dashboard.trend.empty', 'en-US', 'No orders in this window', 200, 'admin', 'admin/dashboard.html: 销售趋势空态', 1, now(), now()),
('admin.dashboard.trend.empty', 'zh-CN', '这段时间没有订单', 200, 'admin', 'admin/dashboard.html: 销售趋势空态', 1, now(), now()),
('admin.dashboard.top.title', 'en-US', 'Top products (last 7 days)', 200, 'admin', 'admin/dashboard.html: 热销商品块标题', 1, now(), now()),
('admin.dashboard.top.title', 'zh-CN', '热销商品（近 7 天）', 200, 'admin', 'admin/dashboard.html: 热销商品块标题', 1, now(), now()),
('admin.dashboard.top.empty', 'en-US', 'No paid orders in this window', 200, 'admin', 'admin/dashboard.html: 热销商品空态', 1, now(), now()),
('admin.dashboard.top.empty', 'zh-CN', '这段时间没有已付款的订单', 200, 'admin', 'admin/dashboard.html: 热销商品空态', 1, now(), now()),
('admin.dashboard.top.quantity', 'en-US', 'Sold', 200, 'admin', 'admin/dashboard.html: 热销商品表头（销量）', 1, now(), now()),
('admin.dashboard.top.quantity', 'zh-CN', '销量', 200, 'admin', 'admin/dashboard.html: 热销商品表头（销量）', 1, now(), now()),
('admin.dashboard.top.amount', 'en-US', 'Amount', 200, 'admin', 'admin/dashboard.html: 热销商品表头（销售额）', 1, now(), now()),
('admin.dashboard.top.amount', 'zh-CN', '销售额', 200, 'admin', 'admin/dashboard.html: 热销商品表头（销售额）', 1, now(), now()),
('admin.dashboard.content.title', 'en-US', 'Site content', 200, 'admin', 'admin/dashboard.html: 站点内容小节标题', 1, now(), now()),
('admin.dashboard.content.title', 'zh-CN', '站点内容', 200, 'admin', 'admin/dashboard.html: 站点内容小节标题', 1, now(), now()),
('admin.dashboard.overview.unavailable', 'en-US', 'Some overview data is not available yet (the module is not wired).', 200, 'admin', 'admin/dashboard.html: 概览数据源未接线时的提示', 1, now(), now()),
('admin.dashboard.overview.unavailable', 'zh-CN', '部分概览数据暂不可用（对应模块未接线）。', 200, 'admin', 'admin/dashboard.html: 概览数据源未接线时的提示', 1, now(), now()),
('admin.dashboard.action.orders', 'en-US', 'Orders', 200, 'admin', 'admin/dashboard.html: 页头动作（订单管理）', 1, now(), now()),
('admin.dashboard.action.orders', 'zh-CN', '订单管理', 200, 'admin', 'admin/dashboard.html: 页头动作（订单管理）', 1, now(), now()),
('admin.dashboard.kpi.salesNote', 'en-US', 'Net of received refunds', 200, 'admin', 'admin/dashboard.html: 销售额卡片小注', 1, now(), now()),
('admin.dashboard.kpi.salesNote', 'zh-CN', '净额（已扣已收货退款）', 200, 'admin', 'admin/dashboard.html: 销售额卡片小注', 1, now(), now()),
('admin.dashboard.kpi.articleViewsNote', 'en-US', 'Article pages only', 200, 'admin', 'admin/dashboard.html: 文章浏览卡片小注', 1, now(), now()),
('admin.dashboard.kpi.articleViewsNote', 'zh-CN', '只看文章页路径', 200, 'admin', 'admin/dashboard.html: 文章浏览卡片小注', 1, now(), now()),
('admin.dashboard.trend.ordersUnit', 'en-US', 'orders', 200, 'admin', 'admin/dashboard.html: 趋势图柱子的悬浮说明（单量单位）', 1, now(), now()),
('admin.dashboard.trend.ordersUnit', 'zh-CN', '单', 200, 'admin', 'admin/dashboard.html: 趋势图柱子的悬浮说明（单量单位）', 1, now(), now()),
('admin.dashboard.top.col.rank', 'en-US', 'Rank', 200, 'admin', 'admin/dashboard.html: 热销商品表头（名次）', 1, now(), now()),
('admin.dashboard.top.col.rank', 'zh-CN', '名次', 200, 'admin', 'admin/dashboard.html: 热销商品表头（名次）', 1, now(), now()),
('admin.dashboard.top.col.product', 'en-US', 'Product', 200, 'admin', 'admin/dashboard.html: 热销商品表头（商品）', 1, now(), now()),
('admin.dashboard.top.col.product', 'zh-CN', '商品', 200, 'admin', 'admin/dashboard.html: 热销商品表头（商品）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 口径说明覆盖（旧文案讲的是「工程数 / 页面数」，新概览要先讲清日界与金额口径）。
WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.dashboard.intro', 'zh-CN', '统计口径：KPI 与趋势按 UTC 日界（与访问统计同口径），“今日”指 UTC 当天。销售额为净额 = 计入消费的订单金额 − 已实际收货的退款额，货币为站点默认货币。「热销商品」只统计已付款/已发货/已完成的订单，商品名与 SKU 是下单当时的快照，金额不含退款分摊。全部为实时查询，不做缓存。'),
  ('admin.dashboard.intro', 'en-US', 'How these numbers are computed: KPIs and the trend use UTC day boundaries (the same as analytics), so "today" means the UTC day. Sales is net = paid-order amounts minus refunds actually received, in the site default currency. Top products counts only paid/shipped/completed orders; names and SKUs are the snapshots taken at order time and amounts exclude refund allocation. Everything is queried live, nothing is cached.')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key
   AND s.lang = v.lang
   AND s.item_value <> v.item_value;
