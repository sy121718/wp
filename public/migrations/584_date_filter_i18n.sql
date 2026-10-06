-- 584 · 后台统一时间筛选条（admin/partials/date_filter.html）的词条（BIZ-1）。
--
-- 12 个 key × 2 语言 = 24 行。全部在 admin.common.filter.* 命名空间下：
-- 组件是跨模块共用件，不归某一个业务模块，所以不能挂在 admin.order.* / admin.dashboard.* 下面。
--
-- admin.common.filter.submit（查询）与 admin.common.filter.reset（重置）**不在本批**：
-- 它们已经被更早的迁移种过（本批首次运行 i18n 门禁时只报了这 12 个）。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.common.filter.preset', 'zh-CN', '快捷区间'),
('admin.common.filter.preset', 'en-US', 'Quick range'),
('admin.common.filter.preset.custom', 'zh-CN', '自定义'),
('admin.common.filter.preset.custom', 'en-US', 'Custom'),
('admin.common.filter.preset.today', 'zh-CN', '今日'),
('admin.common.filter.preset.today', 'en-US', 'Today'),
('admin.common.filter.preset.yesterday', 'zh-CN', '昨日'),
('admin.common.filter.preset.yesterday', 'en-US', 'Yesterday'),
('admin.common.filter.preset.week', 'zh-CN', '本周'),
('admin.common.filter.preset.week', 'en-US', 'This week'),
('admin.common.filter.preset.month', 'zh-CN', '本月'),
('admin.common.filter.preset.month', 'en-US', 'This month'),
('admin.common.filter.preset.lastMonth', 'zh-CN', '上月'),
('admin.common.filter.preset.lastMonth', 'en-US', 'Last month'),
('admin.common.filter.preset.days7', 'zh-CN', '最近 7 天'),
('admin.common.filter.preset.days7', 'en-US', 'Last 7 days'),
('admin.common.filter.preset.days30', 'zh-CN', '最近 30 天'),
('admin.common.filter.preset.days30', 'en-US', 'Last 30 days'),
('admin.common.filter.preset.year', 'zh-CN', '本年'),
('admin.common.filter.preset.year', 'en-US', 'This year'),
('admin.common.filter.from', 'zh-CN', '开始日期'),
('admin.common.filter.from', 'en-US', 'Start date'),
('admin.common.filter.to', 'zh-CN', '结束日期'),
('admin.common.filter.to', 'en-US', 'End date')
ON CONFLICT (item_key, lang) DO NOTHING;
