-- 571 · 概览页的 AI 提问区与合并后的「销售数据」卡词条。
--
-- 为什么单开一条：551 已经在开发库与其他库上执行过，它的 ConditionSQL 在那
-- 些库上已经满足 → **整条会被判为已完成而跳过**，新加的 key 永远进不去。
-- 迁移一旦发布过就不能追加内容（545 / 546 / 570 同一课）。
--
-- 概览页结构变更（六卡合并为三卡 + 顶部 AI 提问框）：
--   · 「销售数据」是卡合并后新出现的标签（订单数/净销售额/待发货/待付款四个数字
--     合成一张卡，卡内标签沿用 551 已有的那几条 key）；
--   · AI 提问框只新增标题与占位文案两条 —— 发送/停止/思考中/思考过程/全部会话
--     直接复用 568 与 570 已 seed 的 admin.ai.fab.* 与 ai.fab.*（都是幂等 key，
--     复用而不是另起一套，避免同一句话维护两份译文）。
--
-- 幂等：ON CONFLICT DO NOTHING；判据取本批自己的代表 key。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.dashboard.kpi.salesData', 'zh-CN', '销售数据'),
('admin.dashboard.kpi.salesData', 'en-US', 'Sales'),
('admin.dashboard.ai.title', 'zh-CN', '问 AI（经营数据）'),
('admin.dashboard.ai.title', 'en-US', 'Ask AI (business data)'),
('admin.dashboard.ai.placeholder', 'zh-CN', '问点什么：这两周卖得怎么样？哪几单还没发货？（Enter 发送，Shift+Enter 换行）'),
('admin.dashboard.ai.placeholder', 'en-US', 'Ask anything: how were sales these two weeks? Which orders have not shipped? (Enter to send, Shift+Enter for a new line)')
ON CONFLICT (item_key, lang) DO NOTHING;
