-- 578 · 趋势图粒度切换（小时 / 天 / 周 / 月）。
--
-- 概览页的趋势卡新增一排粒度按钮与随之变化的卡标题。默认粒度仍**跟着区间走**
-- （1~2 天按小时、3~31 天按天、更长按周），按钮是给「我就要这个区间 + 那个粒度」
-- 的人用的覆盖入口。
--
-- 为什么按钮都叫「小时 / 天 / 周 / 月」这么短：它们是一组分段控件，四个词并排；
-- 写成「按小时 / 按天 / …」会重复四遍「按」字，而每组件的语义已由 aria-label
-- （admin.dashboard.trend.granularity）交代。
--
-- 旧的 admin.dashboard.trend.title / titleWeekly 不再被模板引用（改为按粒度取
-- title.<granularity>）。这里**不删**那两条：删词条要么写 DELETE、要么留一条
-- 指向已不存在 key 的账单，而它们没有任何副作用 —— 需要收口时单独一批处理。
--
-- 幂等：ON CONFLICT DO NOTHING。这是一批**新增**词条（不覆盖存量取值），
-- 与 577 的「改值」不同。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.dashboard.trend.granularity', 'zh-CN', '聚合粒度'),
('admin.dashboard.trend.granularity', 'en-US', 'Granularity'),
('admin.dashboard.trend.granularity.hour', 'zh-CN', '小时'),
('admin.dashboard.trend.granularity.hour', 'en-US', 'Hour'),
('admin.dashboard.trend.granularity.day', 'zh-CN', '天'),
('admin.dashboard.trend.granularity.day', 'en-US', 'Day'),
('admin.dashboard.trend.granularity.week', 'zh-CN', '周'),
('admin.dashboard.trend.granularity.week', 'en-US', 'Week'),
('admin.dashboard.trend.granularity.month', 'zh-CN', '月'),
('admin.dashboard.trend.granularity.month', 'en-US', 'Month'),
('admin.dashboard.trend.title.hour', 'zh-CN', '趋势（按小时）'),
('admin.dashboard.trend.title.hour', 'en-US', 'Trend (hourly)'),
('admin.dashboard.trend.title.day', 'zh-CN', '趋势（按天）'),
('admin.dashboard.trend.title.day', 'en-US', 'Trend (daily)'),
('admin.dashboard.trend.title.week', 'zh-CN', '趋势（按周）'),
('admin.dashboard.trend.title.week', 'en-US', 'Trend (weekly)'),
('admin.dashboard.trend.title.month', 'zh-CN', '趋势（按月）'),
('admin.dashboard.trend.title.month', 'en-US', 'Trend (monthly)')
ON CONFLICT (item_key, lang) DO NOTHING;
