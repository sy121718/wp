-- 562 · 群组留存页的词条（admin/user/customer_cohort.html）
--
-- 背景：客户目录第四页 —— 群组留存矩阵（行 = 首单所在月，列 = 相对月序号）。
--
-- 覆盖：19 个新 key × 2 语言 = 38 条。
--
-- 时间档位那五条（admin.dashboard.range.*）刻意不重复 seed：与仪表盘 / 客户概览 / RFM 共用。
--
-- 页头说明拆成多个 key（中间要夹 <strong>）：整句塞进一个词条就没法加粗。
-- 「空白 = 时间还没到，不是 0%」这句必须留在页面上 —— 不写的话最新几个月的一片空
-- 一定会被读成断崖式流失，而那不会产生任何报错。
--
-- noRows 的文案在**同一个批次内**改过一次（原为「没有可比对的群」）。判断依据：
-- 这条 key 是本批首次引入、除本次的开发库之外没有任何库跑过这个文件，所以改文件 +
-- 同步开发库是干净的；**一旦本文件已发布过，就必须新开一个迁移做 UPDATE**，
-- 否则已执行过它的库会因 ConditionSQL 判为完成而跳过，新旧库文案长期不一致。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customer.cohort.title', 'en-US', 'Cohort retention', 200, 'admin', 'admin/user/customer_cohort.html: 页标题', 1, now(), now()),
('admin.customer.cohort.title', 'zh-CN', '群组留存', 200, 'admin', 'admin/user/customer_cohort.html: 页标题', 1, now(), now()),
('admin.customer.cohort.help.label', 'en-US', 'View help', 200, 'admin', 'admin/user/customer_cohort.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.customer.cohort.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/user/customer_cohort.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.customer.cohort.hint.read.lead', 'en-US', 'How to read this table: ', 200, 'admin', 'admin/user/customer_cohort.html: 说明（读法前段）', 1, now(), now()),
('admin.customer.cohort.hint.read.lead', 'zh-CN', '怎么读这张表：', 200, 'admin', 'admin/user/customer_cohort.html: 说明（读法前段）', 1, now(), now()),
('admin.customer.cohort.hint.read.strong', 'en-US', 'each row is one batch of people', 200, 'admin', 'admin/user/customer_cohort.html: 说明（读法加粗）', 1, now(), now()),
('admin.customer.cohort.hint.read.strong', 'zh-CN', '每一行是一批人', 200, 'admin', 'admin/user/customer_cohort.html: 说明（读法加粗）', 1, now(), now()),
('admin.customer.cohort.hint.read.tail', 'en-US', ' — their first order all falls in the same calendar month. Reading that row left to right shows whether they came back: the first month is 100% (their first order is in it), and each cell to the right is the share of that batch who bought again in that month.', 200, 'admin', 'admin/user/customer_cohort.html: 说明（读法后段）', 1, now(), now()),
('admin.customer.cohort.hint.read.tail', 'zh-CN', '——他们的第一单都落在同一个自然月。横向看这一行，就是这批人后来还回不回来：首月是 100%（他们的第一单就在那个月），往右每一格是该月又有购买的人占本批人的比例。', 200, 'admin', 'admin/user/customer_cohort.html: 说明（读法后段）', 1, now(), now()),
('admin.customer.cohort.hint.blank.strong', 'en-US', 'Blank cells mean the month has not arrived yet', 200, 'admin', 'admin/user/customer_cohort.html: 说明（空格加粗）', 1, now(), now()),
('admin.customer.cohort.hint.blank.strong', 'zh-CN', '空白的格子是「时间还没到」', 200, 'admin', 'admin/user/customer_cohort.html: 说明（空格加粗）', 1, now(), now()),
('admin.customer.cohort.hint.blank.tail', 'en-US', ', not 0%. The last few months will have many blanks and that is normal; showing them as 0% would look like customers churning off a cliff when in fact the month simply has not happened yet.', 200, 'admin', 'admin/user/customer_cohort.html: 说明（空格后段）', 1, now(), now()),
('admin.customer.cohort.hint.blank.tail', 'zh-CN', '，不是 0%。最后几个月的空格会很多，那是正常的；显示成 0% 的话看起来会像客户断崖式流失，而真实原因只是还没到那个月。', 200, 'admin', 'admin/user/customer_cohort.html: 说明（空格后段）', 1, now(), now()),
('admin.customer.cohort.hint.denom.strong', 'en-US', 'The denominator is each batch''s own size', 200, 'admin', 'admin/user/customer_cohort.html: 说明（分母加粗）', 1, now(), now()),
('admin.customer.cohort.hint.denom.strong', 'zh-CN', '分母是本批人数', 200, 'admin', 'admin/user/customer_cohort.html: 说明（分母加粗）', 1, now(), now()),
('admin.customer.cohort.hint.denom.tail', 'en-US', ', not the total customer count — so rows can be compared directly. Only orders that count towards sales are included; guest orders (no account) are not.', 200, 'admin', 'admin/user/customer_cohort.html: 说明（分母后段）', 1, now(), now()),
('admin.customer.cohort.hint.denom.tail', 'zh-CN', '，不是全部客户数 —— 所以不同行之间可以直接比。只统计计入消费的订单；游客单（没有账号）不计入。', 200, 'admin', 'admin/user/customer_cohort.html: 说明（分母后段）', 1, now(), now()),
('admin.customer.cohort.col.cohort', 'en-US', 'First order month', 200, 'admin', 'admin/user/customer_cohort.html: 表头（首单月份）', 1, now(), now()),
('admin.customer.cohort.col.cohort', 'zh-CN', '首单月份', 200, 'admin', 'admin/user/customer_cohort.html: 表头（首单月份）', 1, now(), now()),
('admin.customer.cohort.col.size', 'en-US', 'Size', 200, 'admin', 'admin/user/customer_cohort.html: 表头（本群人数）', 1, now(), now()),
('admin.customer.cohort.col.size', 'zh-CN', '人数', 200, 'admin', 'admin/user/customer_cohort.html: 表头（本群人数）', 1, now(), now()),
('admin.customer.cohort.col.first', 'en-US', 'First month', 200, 'admin', 'admin/user/customer_cohort.html: 列头（第 0 列）', 1, now(), now()),
('admin.customer.cohort.col.first', 'zh-CN', '首月', 200, 'admin', 'admin/user/customer_cohort.html: 列头（第 0 列）', 1, now(), now()),
('admin.customer.cohort.col.monthSuffix', 'en-US', 'mo', 200, 'admin', 'admin/user/customer_cohort.html: 列头「+N月」的后缀', 1, now(), now()),
('admin.customer.cohort.col.monthSuffix', 'zh-CN', '月', 200, 'admin', 'admin/user/customer_cohort.html: 列头「+N月」的后缀', 1, now(), now()),
('admin.customer.cohort.empty', 'en-US', 'No new customers in this range: either no site project exists yet, or no new customer ordered here. Try another range.', 200, 'admin', 'admin/user/customer_cohort.html: 无数据空态', 1, now(), now()),
('admin.customer.cohort.empty', 'zh-CN', '这段时间里没有新客户：可能是还没有站点工程，也可能确实没有新客户在这里下过单。换个时间段试试。', 200, 'admin', 'admin/user/customer_cohort.html: 无数据空态', 1, now(), now()),
('admin.customer.cohort.unavailable', 'en-US', 'Cohort retention is temporarily unavailable (data not wired up).', 200, 'admin', 'admin/user/customer_cohort.html: 端口缺席时的说明', 1, now(), now()),
('admin.customer.cohort.unavailable', 'zh-CN', '群组留存暂时不可用（数据没接上）。', 200, 'admin', 'admin/user/customer_cohort.html: 端口缺席时的说明', 1, now(), now()),
('admin.customer.cohort.noRows', 'en-US', 'No new customers in this range: nobody first ordered in it.', 200, 'admin', 'admin/user/customer_cohort.html: 有观测窗但没有群', 1, now(), now()),
('admin.customer.cohort.noRows', 'zh-CN', '这段时间里没有新客户：没有人的第一单落在这里。', 200, 'admin', 'admin/user/customer_cohort.html: 有观测窗但没有群', 1, now(), now()),
('admin.customer.cohort.scope', 'en-US', 'Range: ', 200, 'admin', 'admin/user/customer_cohort.html: 页脚区间前缀', 1, now(), now()),
('admin.customer.cohort.scope', 'zh-CN', '区间：', 200, 'admin', 'admin/user/customer_cohort.html: 页脚区间前缀', 1, now(), now()),
('admin.customer.cohort.totals', 'en-US', 'cohorts', 200, 'admin', 'admin/user/customer_cohort.html: 页脚（群数标签）', 1, now(), now()),
('admin.customer.cohort.totals', 'zh-CN', '群数', 200, 'admin', 'admin/user/customer_cohort.html: 页脚（群数标签）', 1, now(), now()),
('admin.customer.cohort.totalCustomers', 'en-US', 'new customers in total', 200, 'admin', 'admin/user/customer_cohort.html: 页脚（人数标签）', 1, now(), now()),
('admin.customer.cohort.totalCustomers', 'zh-CN', '新客户合计', 200, 'admin', 'admin/user/customer_cohort.html: 页脚（人数标签）', 1, now(), now());
