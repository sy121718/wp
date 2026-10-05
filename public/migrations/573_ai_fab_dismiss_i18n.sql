-- 573 · 概览页 AI 区的「收起回答」与悬浮球的「回概览继续」。
--
-- 为什么单开一条：571 已经在开发库与其他库上执行过，它的 ConditionSQL 在那
-- 些库上已经满足 → **整条会被判为已完成而跳过**，新加的 key 永远进不去。
-- 迁移一旦发布过就不能追加内容（545 / 546 / 570 / 571 同一课）。
--
-- 两条 key 是「同一个入口的两个方向」：
--   · admin.dashboard.ai.dismiss：在概览页把回答收起来，交还卡片与图表；
--   · admin.ai.fab.backToDashboard：在别的页面上回到概览页接着问。
--     它们服务的是同一件事 —— 概览的提问框与全局悬浮球是同一段会话，
--     只是一个在页面里、一个浮在角落。
--
-- 幂等：ON CONFLICT DO NOTHING；判据取本批自己的代表 key。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.dashboard.ai.dismiss', 'zh-CN', '收起回答'),
('admin.dashboard.ai.dismiss', 'en-US', 'Hide answer'),
('admin.ai.fab.backToDashboard', 'zh-CN', '回概览继续'),
('admin.ai.fab.backToDashboard', 'en-US', 'Back to dashboard')
ON CONFLICT (item_key, lang) DO NOTHING;
