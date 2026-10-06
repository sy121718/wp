-- 583 · 销售概览的月度趋势改折线图后调整词条（BIZ-1）。
--
-- 三件事，同一批做完：
--   ① 新 key：月度趋势图的 aria-label（图从柱状改成折线，标题要能说清「这是销售额趋势」）。
--   ② 改文案：monthly.empty 原文写的是「无可画的柱子」—— 图已经不是柱状了，留着会让
--      空态与图形对不上（读的人会去找一根并不存在的柱子）。
--   ③ 删孤儿：582 为「两排卡片的分组标题（销售 / 客户）」种的两个 key 不再被任何模板引用 ——
--      卡片合成一排四张之后没有分组标题了。同批把 582 的 INSERT 与 ConditionSQL 一起改掉：
--      否则 582 的门槛（3 个 key × 2 语言 ≥ 6 行）永远不满足，每次启动都会重跑它，
--      把这里删掉的两条又插回来。
--
-- month.*（月度合计）与 mix.*（新客 / 回头客 / 游客）两组 key 不动：折线图继续用它们
-- 作图例与悬停读数。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.order.sales.monthly.title', 'zh-CN', '月度销售额趋势'),
('admin.order.sales.monthly.title', 'en-US', 'Monthly sales trend')
ON CONFLICT (item_key, lang) DO NOTHING;

-- 改文案用 UPDATE（不是 INSERT）：这两个词条 582 已经种过，ON CONFLICT DO NOTHING 会让
-- 新文案永远进不去 —— 而那正是「迁移写对了但界面上还是旧字」的经典来源。
UPDATE sys_i18n SET item_value = '这段时间没有计入消费的订单，趋势暂无可画的线。'
 WHERE item_key = 'admin.order.sales.monthly.empty' AND lang = 'zh-CN';
UPDATE sys_i18n SET item_value = 'No paid orders in this window, so there is nothing to plot yet.'
 WHERE item_key = 'admin.order.sales.monthly.empty' AND lang = 'en-US';

DELETE FROM sys_i18n
 WHERE item_key IN ('admin.order.sales.group.sales', 'admin.order.sales.group.customers');
