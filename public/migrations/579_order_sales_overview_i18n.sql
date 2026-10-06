-- 579 · 销售概览页（/admin/orders/overview）的词条。
--
-- 本页是整页只读报表：筛选条 + 两排统计卡 + 环比 + 月度趋势。
-- 文案分四类，前缀 `admin.order.sales.`：
--   label.*  卡片标题（已译后直接渲染，不再套 t）
--   note.*   卡片口径说明（走 title 悬浮，不占卡面；含一个 %d 占位）
--   其余     页面骨架与表头
--
-- **为什么 note.* 里带 %d 的形式串能进 i18n**：它是拼出来的（含动态数字），
-- 由视图层 `fmt.Sprintf(tr(key, fallback), n)` 填；占位符个数由语言各写各的，
-- 所以换了语言也不会漏参 —— 代价是两条语言的占位符个数必须一致（本批都是 1 个）。
--
-- 状态快捷项复用订单模块既有词条（admin.orders.status.*），本批只补「全部」那一条
-- （admin.order.sales.status.all）：订单模块里没有「不筛状态」的表达，
-- 而它在本页是一个**选项值**（空串），必须有个名字。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
-- ===== 页面骨架 =====
('admin.order.sales.title', 'zh-CN', '销售概览'),
('admin.order.sales.title', 'en-US', 'Sales Overview'),
('admin.order.sales.desc', 'zh-CN', '按区间看销量、客单与客户结构；口径只含已付款、已发货、已完成的订单，取消与退款不计入。'),
('admin.order.sales.desc', 'en-US', 'Volume, order value and customer mix for a date range. Only paid, shipped and completed orders count; cancelled and refunded ones are excluded.'),
('admin.order.sales.toOrders', 'zh-CN', '订单列表'),
('admin.order.sales.toOrders', 'en-US', 'Orders'),
('admin.order.sales.field.from', 'zh-CN', '开始日期'),
('admin.order.sales.field.from', 'en-US', 'From'),
('admin.order.sales.field.to', 'zh-CN', '结束日期'),
('admin.order.sales.field.to', 'en-US', 'To'),
('admin.order.sales.field.status', 'zh-CN', '订单状态'),
('admin.order.sales.field.status', 'en-US', 'Order status'),
('admin.order.sales.field.monthly', 'zh-CN', '趋势月数'),
('admin.order.sales.field.monthly', 'en-US', 'Trend months'),
('admin.order.sales.filter.submit', 'zh-CN', '应用'),
('admin.order.sales.filter.submit', 'en-US', 'Apply'),
('admin.order.sales.status.all', 'zh-CN', '全部'),
('admin.order.sales.status.all', 'en-US', 'All'),
('admin.order.sales.loadFailed', 'zh-CN', '数据没能读出来，稍后重试。'),
('admin.order.sales.loadFailed', 'en-US', 'Could not load the data. Try again later.'),
('admin.order.sales.empty', 'zh-CN', '这段时间没有计入消费的订单。'),
('admin.order.sales.empty', 'en-US', 'No counted orders in this range.'),
('admin.order.sales.emptyHint', 'zh-CN', '换一个区间，或把状态筛成「全部」。'),
('admin.order.sales.emptyHint', 'en-US', 'Try another range, or set status to All.'),

-- ===== 卡片标题 =====
('admin.order.sales.label.orders', 'zh-CN', '订单数'),
('admin.order.sales.label.orders', 'en-US', 'Orders'),
('admin.order.sales.label.sales', 'zh-CN', '销售额'),
('admin.order.sales.label.sales', 'en-US', 'Sales'),
('admin.order.sales.label.aov', 'zh-CN', '平均订单价值'),
('admin.order.sales.label.aov', 'en-US', 'Avg order value'),
('admin.order.sales.label.units', 'zh-CN', '平均每单件数'),
('admin.order.sales.label.units', 'en-US', 'Avg items per order'),
('admin.order.sales.label.customers', 'zh-CN', '下单客户'),
('admin.order.sales.label.customers', 'en-US', 'Customers'),
('admin.order.sales.label.new', 'zh-CN', '新客户'),
('admin.order.sales.label.new', 'en-US', 'New customers'),
('admin.order.sales.label.returning', 'zh-CN', '回头客户'),
('admin.order.sales.label.returning', 'en-US', 'Returning customers'),
('admin.order.sales.label.acv', 'zh-CN', '平均客户价值'),
('admin.order.sales.label.acv', 'en-US', 'Avg customer value'),

-- ===== 卡片口径（悬浮，1 个 %d 占位）=====
('admin.order.sales.note.orders', 'zh-CN', '含没有商品明细的订单；共 %d 行明细'),
('admin.order.sales.note.orders', 'en-US', 'Includes orders without line items; %d rows in total'),
('admin.order.sales.note.sales', 'zh-CN', '商品行实付合计，不含退款分摊（净销售额是另一个口径）'),
('admin.order.sales.note.sales', 'en-US', 'Sum of line totals, before refund allocation (net sales is a different figure)'),
('admin.order.sales.note.aov', 'zh-CN', '销售额 ÷ 订单数（AOV）'),
('admin.order.sales.note.aov', 'en-US', 'Sales ÷ orders (AOV)'),
('admin.order.sales.note.units', 'zh-CN', '商品明细行数 ÷ 订单数；不等于件数（一行可能多件），本区间共 %d 件'),
('admin.order.sales.note.units', 'en-US', 'Line rows ÷ orders; not the unit count (one row may hold many), %d units in this range'),
('admin.order.sales.note.customers', 'zh-CN', '按账号去重；游客单不计入（没有账号可归）'),
('admin.order.sales.note.customers', 'en-US', 'Deduplicated by account; guest orders are excluded (no account to attribute)'),
('admin.order.sales.note.new', 'zh-CN', '首单落在本区间内（不看注册时间）'),
('admin.order.sales.note.new', 'en-US', 'First order falls inside this range (registration time is ignored)'),
('admin.order.sales.note.returning', 'zh-CN', '首单在区间之前、区间内又下单；复购率 %.1f%%（%d 人下了 2 单以上）'),
('admin.order.sales.note.returning', 'en-US', 'First order before, another inside; repurchase rate %.1f%% (%d with two or more)'),
('admin.order.sales.note.acv', 'zh-CN', '销售额 ÷ 下单客户数（ACV）；分母是人不是单，与 AOV 不同'),
('admin.order.sales.note.acv', 'en-US', 'Sales ÷ customers (ACV); the denominator is people, not orders — unlike AOV'),

-- ===== 环比 =====
('admin.order.sales.compare', 'zh-CN', '环比上一段等长区间'),
('admin.order.sales.compare', 'en-US', 'Versus the previous equal-length range'),
('admin.order.sales.compareHint', 'zh-CN', '上一段与本段同样长，不与自然月对比'),
('admin.order.sales.compareHint', 'en-US', 'Previous window is the same length, not a calendar month'),
('admin.order.sales.compare.metric', 'zh-CN', '指标'),
('admin.order.sales.compare.metric', 'en-US', 'Metric'),
('admin.order.sales.compare.prev', 'zh-CN', '上一期'),
('admin.order.sales.compare.prev', 'en-US', 'Previous'),
('admin.order.sales.compare.change', 'zh-CN', '变化'),
('admin.order.sales.compare.change', 'en-US', 'Change'),
('admin.order.sales.compare.noBase', 'zh-CN', '上期无数据'),
('admin.order.sales.compare.noBase', 'en-US', 'No base period'),

-- ===== 月度趋势 =====
('admin.order.sales.monthly', 'zh-CN', '月度趋势'),
('admin.order.sales.monthly', 'en-US', 'Monthly trend'),
('admin.order.sales.monthlyHint', 'zh-CN', '固定回看，与上面的筛选区间无关'),
('admin.order.sales.monthlyHint', 'en-US', 'Fixed lookback, independent of the filter range')
ON CONFLICT (item_key, lang) DO NOTHING;
