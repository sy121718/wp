-- 582 · 销售概览页的趋势空态词条（BIZ-1）。
--
-- 原本还种了两个「分组标题（销售 / 客户）」key，583 已删除它们（卡片合成一排四张、
-- 不再有分组标题），这里的 INSERT 与注册文件的 ConditionSQL 同批收窄 —— 只留空态文案。
-- 该文案的值在 583 里被 UPDATE 成折线图版本（原文写的是「无可画的柱子」）。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.order.sales.monthly.empty', 'zh-CN', '这段时间没有计入消费的订单，趋势暂无可画的柱子。'),
('admin.order.sales.monthly.empty', 'en-US', 'No paid orders in this window, so there is nothing to plot yet.')
ON CONFLICT (item_key, lang) DO NOTHING;
