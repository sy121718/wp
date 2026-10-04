-- 560 · RFM 分析页的词条（admin/user/customer_rfm.html）
--
-- 背景：客户目录第三页 —— RFM 分层（R 最近下单 / F 区间频次 / M 区间消费额，各按五分位 1-5 分）。
--
-- 覆盖：26 个新 key × 2 语言 = 52 条（ON CONFLICT DO NOTHING）。
--
-- 时间档位那五条（admin.dashboard.range.*）刻意不重复 seed：这一页与仪表盘、客户概览
-- 共用同一批档位，各写一套的话迟早出现「两页的『本周』不是同一个意思」。
--
-- 页头说明拆成多个 key（中间要夹 <strong>）：整句塞进一个词条就没法加粗。
-- 「分数是相对的」这句必须留在页面上（而不是只写在代码注释里）——
-- 不写的话五分位一定会被读成绝对等级，而那种误读不会产生任何报错。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customer.rfm.title', 'en-US', 'RFM analysis', 200, 'admin', 'admin/user/customer_rfm.html: 页标题', 1, now(), now()),
('admin.customer.rfm.title', 'zh-CN', 'RFM 分析', 200, 'admin', 'admin/user/customer_rfm.html: 页标题', 1, now(), now()),
('admin.customer.rfm.help.label', 'en-US', 'View help', 200, 'admin', 'admin/user/customer_rfm.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.customer.rfm.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/user/customer_rfm.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.customer.rfm.hint.score.lead', 'en-US', 'All three scores are ', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分数口径前段）', 1, now(), now()),
('admin.customer.rfm.hint.score.lead', 'zh-CN', 'R / F / M 三个分数都是', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分数口径前段）', 1, now(), now()),
('admin.customer.rfm.hint.score.strong', 'en-US', 'quintiles (relative)', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分数口径加粗词）', 1, now(), now()),
('admin.customer.rfm.hint.score.strong', 'zh-CN', '五分位（相对分）', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分数口径加粗词）', 1, now(), now()),
('admin.customer.rfm.hint.score.tail', 'en-US', ': the customers in this range are ranked, the top 20% get 5 and the bottom 20% get 1. The same person gets different scores in a different range — they are not absolute grades, and comparing them across ranges means nothing.', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分数口径后段）', 1, now(), now()),
('admin.customer.rfm.hint.score.tail', 'zh-CN', '：按这段时间里下单的这批人排队，前 20% 得 5 分、后 20% 得 1 分。同一个人换一个时间段，分数会变 —— 它们不是绝对等级，换个区间比分数没有意义。', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分数口径后段）', 1, now(), now()),
('admin.customer.rfm.hint.def.strong', 'en-US', 'What the letters mean', 200, 'admin', 'admin/user/customer_rfm.html: 说明（定义加粗词）', 1, now(), now()),
('admin.customer.rfm.hint.def.strong', 'zh-CN', '三个字母的意思', 200, 'admin', 'admin/user/customer_rfm.html: 说明（定义加粗词）', 1, now(), now()),
('admin.customer.rfm.hint.def.tail', 'en-US', ': R = how long since the last order (more recent scores higher); F = number of orders in this range; M = net spend in this range. The first two count paid-and-later orders only; guest orders (no account) are excluded.', 200, 'admin', 'admin/user/customer_rfm.html: 说明（定义后段）', 1, now(), now()),
('admin.customer.rfm.hint.def.tail', 'zh-CN', '：R = 最近一次下单距今多久（越近分越高）；F = 这段时间里下了几单；M = 这段时间的净消费额。前两个都只看计入消费的订单，游客单（没有账号）不计入。', 200, 'admin', 'admin/user/customer_rfm.html: 说明（定义后段）', 1, now(), now()),
('admin.customer.rfm.hint.seg.strong', 'en-US', 'Segments', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分段加粗词）', 1, now(), now()),
('admin.customer.rfm.hint.seg.strong', 'zh-CN', '分段', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分段加粗词）', 1, now(), now()),
('admin.customer.rfm.hint.seg.tail', 'en-US', ' come from the total score: 12 or more is high value, 8 or more is potential, the rest is general.', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分段后段）', 1, now(), now()),
('admin.customer.rfm.hint.seg.tail', 'zh-CN', '按三项总分划分：≥12 是高价值，≥8 是潜力，其余是一般。', 200, 'admin', 'admin/user/customer_rfm.html: 说明（分段后段）', 1, now(), now()),
('admin.customer.rfm.empty', 'en-US', 'No customers to analyse in this range: either no site project exists yet, or there are no paid orders. Try another range.', 200, 'admin', 'admin/user/customer_rfm.html: 无数据空态', 1, now(), now()),
('admin.customer.rfm.empty', 'zh-CN', '这段时间里没有可分析的客户：可能是还没有站点工程，也可能确实没有已付款的订单。换个时间段试试。', 200, 'admin', 'admin/user/customer_rfm.html: 无数据空态', 1, now(), now()),
('admin.customer.rfm.scope', 'en-US', 'Range: ', 200, 'admin', 'admin/user/customer_rfm.html: 区间回显前缀', 1, now(), now()),
('admin.customer.rfm.scope', 'zh-CN', '范围：', 200, 'admin', 'admin/user/customer_rfm.html: 区间回显前缀', 1, now(), now()),
('admin.customer.rfm.col.customer', 'en-US', 'Customer', 200, 'admin', 'admin/user/customer_rfm.html: 表头（客户）', 1, now(), now()),
('admin.customer.rfm.col.customer', 'zh-CN', '客户', 200, 'admin', 'admin/user/customer_rfm.html: 表头（客户）', 1, now(), now()),
('admin.customer.rfm.col.lastOrder', 'en-US', 'Last order', 200, 'admin', 'admin/user/customer_rfm.html: 表头（最近下单）', 1, now(), now()),
('admin.customer.rfm.col.lastOrder', 'zh-CN', '最近下单', 200, 'admin', 'admin/user/customer_rfm.html: 表头（最近下单）', 1, now(), now()),
('admin.customer.rfm.col.recency', 'en-US', 'Since', 200, 'admin', 'admin/user/customer_rfm.html: 表头（距今）', 1, now(), now()),
('admin.customer.rfm.col.recency', 'zh-CN', '距今', 200, 'admin', 'admin/user/customer_rfm.html: 表头（距今）', 1, now(), now()),
('admin.customer.rfm.col.frequency', 'en-US', 'Orders in range', 200, 'admin', 'admin/user/customer_rfm.html: 表头（区间单数）', 1, now(), now()),
('admin.customer.rfm.col.frequency', 'zh-CN', '区间单数', 200, 'admin', 'admin/user/customer_rfm.html: 表头（区间单数）', 1, now(), now()),
('admin.customer.rfm.col.monetary', 'en-US', 'Spend in range', 200, 'admin', 'admin/user/customer_rfm.html: 表头（区间消费）', 1, now(), now()),
('admin.customer.rfm.col.monetary', 'zh-CN', '区间消费', 200, 'admin', 'admin/user/customer_rfm.html: 表头（区间消费）', 1, now(), now()),
('admin.customer.rfm.col.scores', 'en-US', 'R / F / M', 200, 'admin', 'admin/user/customer_rfm.html: 表头（三项分数）', 1, now(), now()),
('admin.customer.rfm.col.scores', 'zh-CN', 'R / F / M', 200, 'admin', 'admin/user/customer_rfm.html: 表头（三项分数）', 1, now(), now()),
('admin.customer.rfm.col.total', 'en-US', 'Total', 200, 'admin', 'admin/user/customer_rfm.html: 表头（总分）', 1, now(), now()),
('admin.customer.rfm.col.total', 'zh-CN', '总分', 200, 'admin', 'admin/user/customer_rfm.html: 表头（总分）', 1, now(), now()),
('admin.customer.rfm.col.segment', 'en-US', 'Segment', 200, 'admin', 'admin/user/customer_rfm.html: 表头（分段）', 1, now(), now()),
('admin.customer.rfm.col.segment', 'zh-CN', '分段', 200, 'admin', 'admin/user/customer_rfm.html: 表头（分段）', 1, now(), now()),
('admin.customer.rfm.unnamed', 'en-US', 'Customer #', 200, 'admin', 'admin/user/customer_rfm.html: 无姓名时的兜底（后接 id）', 1, now(), now()),
('admin.customer.rfm.unnamed', 'zh-CN', '客户 #', 200, 'admin', 'admin/user/customer_rfm.html: 无姓名时的兜底（后接 id）', 1, now(), now()),
('admin.customer.rfm.daysBefore', 'en-US', ' days ago', 200, 'admin', 'admin/user/customer_rfm.html: 距今天数后缀', 1, now(), now()),
('admin.customer.rfm.daysBefore', 'zh-CN', ' 天前', 200, 'admin', 'admin/user/customer_rfm.html: 距今天数后缀', 1, now(), now()),
('admin.customer.rfm.noRows', 'en-US', 'No customers in this segment for this range.', 200, 'admin', 'admin/user/customer_rfm.html: 该分段无数据', 1, now(), now()),
('admin.customer.rfm.noRows', 'zh-CN', '这个分段在这段时间里没有客户。', 200, 'admin', 'admin/user/customer_rfm.html: 该分段无数据', 1, now(), now()),
('admin.customer.rfm.segment.vip', 'en-US', 'High value', 200, 'admin', 'admin/user/customer_rfm.html: 分段名（高价值）', 1, now(), now()),
('admin.customer.rfm.segment.vip', 'zh-CN', '高价值', 200, 'admin', 'admin/user/customer_rfm.html: 分段名（高价值）', 1, now(), now()),
('admin.customer.rfm.segment.potential', 'en-US', 'Potential', 200, 'admin', 'admin/user/customer_rfm.html: 分段名（潜力）', 1, now(), now()),
('admin.customer.rfm.segment.potential', 'zh-CN', '潜力', 200, 'admin', 'admin/user/customer_rfm.html: 分段名（潜力）', 1, now(), now()),
('admin.customer.rfm.segment.lowValue', 'en-US', 'General', 200, 'admin', 'admin/user/customer_rfm.html: 分段名（一般）', 1, now(), now()),
('admin.customer.rfm.segment.lowValue', 'zh-CN', '一般', 200, 'admin', 'admin/user/customer_rfm.html: 分段名（一般）', 1, now(), now()),
('admin.customer.rfm.tab.all', 'en-US', 'All', 200, 'admin', 'admin/user/customer_rfm.html: 分段徽章（全部）', 1, now(), now()),
('admin.customer.rfm.tab.all', 'zh-CN', '全部', 200, 'admin', 'admin/user/customer_rfm.html: 分段徽章（全部）', 1, now(), now());
