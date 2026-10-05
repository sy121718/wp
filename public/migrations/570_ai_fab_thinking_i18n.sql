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

-- 用户在真机上看到「发出去还在输入框、页面上没有任何进展信号」之后补的两条：
-- 「停止」按钮的文案（流式下按了要能真中断）与请求失败的兜底提示。
-- 同一条迁移内追加是安全的：570 在开发库/其他库上大概率**尚未执行**；
-- 若某库已执行，它的判据（下方 register 里的代表 key 列表）会因为这四条
-- 新 key 而不满足 → 整条重跑 → ON CONFLICT DO NOTHING 保证不冲突。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.ai.fab.stop', 'zh-CN', '停止'),
('admin.ai.fab.stop', 'en-US', 'Stop'),
('admin.ai.fab.failed', 'zh-CN', '这次没能拿到回答，请稍后再试。'),
('admin.ai.fab.failed', 'en-US', 'Could not get an answer this time. Please try again.')
ON CONFLICT (item_key, lang) DO NOTHING;

-- 模板侧的「思考过程」：模板用的是 admin.ai.fab.* 命名空间，而上面那条是
-- handler 侧的 ai.fab.thinkLabel（由 enums 常量给出）。两个命名空间各自独立，
-- 只 seed 一边会让另一边在页面上直接显示出 key 字面量。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.ai.fab.thinkLabel', 'zh-CN', '思考过程'),
('admin.ai.fab.thinkLabel', 'en-US', 'Reasoning')
ON CONFLICT (item_key, lang) DO NOTHING;
