-- 577 · 概览页 KPI 的措辞收口。
--
-- 两处改动，都是「界面上不该出现的东西」：
--
--   1. 「净销售额」→「销售额」。口径（已扣掉已收货退款）本身没变，它是 hover 上的一句话；
--      标签上带「净」字会让读者以为旁边还有一格「毛销售额」在等着对照。
--   2. 注释行不再占卡面 —— 口径说明全部改走 title 悬浮（模板侧改动）。
--      卡片要读的是数字，注解是「想知道才看」的第二层信息；
--      原先每张卡都挂一行小字，等于把四张卡读成了八行。
--
-- 金额前缀同时从货币代码改成符号（CNY 300.50 → ¥300.50）。
-- 符号取自 sys_dict 的 currency 字典（那里每行都有 symbol 列），
-- **不在代码里另建映射** —— 两份必然漂移。
--
-- 幂等：ON CONFLICT DO UPDATE（这两条是改值，不是新增）。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.dashboard.kpi.sales', 'zh-CN', '销售额'),
('admin.dashboard.kpi.sales', 'en-US', 'Sales'),
('admin.dashboard.kpi.salesNote', 'zh-CN', '已扣掉已收货退款的部分'),
('admin.dashboard.kpi.salesNote', 'en-US', 'Net of received refunds')
ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value;
