-- 570 · 悬浮球的「思考中」与「思考过程」两个词条。
--
-- 为什么单开一条而不并进 568：568 已经在开发库与其他库上执行过，
-- 它的 ConditionSQL（枚举 admin.ai.fab.title / admin.ai.fab.toggle）在那些库上
-- 已经满足 → **整条迁移会被判为已完成而跳过**，新加的 key 永远进不去。
-- 迁移一旦发布过就不能追加内容，这是本仓反复踩到的形态（545 / 546 同一课）。
--
-- 幂等：ON CONFLICT DO NOTHING；判据取本批自己的代表 key（含 handler 侧一个，
-- 因为它走的是 enums 常量而不是模板字面量，两种来源都覆盖）。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.ai.fab.thinking', 'zh-CN', '正在思考…（这一步可能要跑几次数据查询，请稍等）'),
('admin.ai.fab.thinking', 'en-US', 'Thinking… (this may take a few data queries)'),
('ai.fab.thinkLabel', 'zh-CN', '思考过程'),
('ai.fab.thinkLabel', 'en-US', 'Reasoning')
ON CONFLICT (item_key, lang) DO NOTHING;
