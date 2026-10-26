-- 635 · 时间筛选条（admin/partials/date_filter.html）改成区间控件后的词条。
--
-- 新增 4 个 key：placeholder（空值占位）/ sep（两端的连接词）/ clear（清除）
-- / preset.halfYear（近半年）。
--
-- 同时把三个旧档位的措辞改成模板兜底里的写法（今日→今天 / 昨日→昨天 / 上月→上个月）：
-- 界面上显示的是**库里的值**，兜底只在 key 缺失时生效 —— 不改的话模板写一套、界面显示另一套。
-- 只改值、不改 key：key 是 URL 之外的东西，但已被 584 种进库，换 key 等于让老译文变孤儿。
--
-- 幂等：INSERT 走 ON CONFLICT DO NOTHING，UPDATE 按 (item_key, lang) 精确定位。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.common.filter.placeholder', 'zh-CN', '选择日期区间'),
('admin.common.filter.placeholder', 'en-US', 'Pick a date range'),
('admin.common.filter.sep', 'zh-CN', '至'),
('admin.common.filter.sep', 'en-US', 'to'),
('admin.common.filter.clear', 'zh-CN', '清除'),
('admin.common.filter.clear', 'en-US', 'Clear'),
('admin.common.filter.preset.halfYear', 'zh-CN', '近半年'),
('admin.common.filter.preset.halfYear', 'en-US', 'Last 6 months')
ON CONFLICT (item_key, lang) DO NOTHING;

UPDATE sys_i18n SET item_value = '今天' WHERE item_key = 'admin.common.filter.preset.today' AND lang = 'zh-CN';
UPDATE sys_i18n SET item_value = '昨天' WHERE item_key = 'admin.common.filter.preset.yesterday' AND lang = 'zh-CN';
UPDATE sys_i18n SET item_value = '上个月' WHERE item_key = 'admin.common.filter.preset.lastMonth' AND lang = 'zh-CN';
