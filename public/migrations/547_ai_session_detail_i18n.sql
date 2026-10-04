-- 547 · 「会话详情」区（sessions.html 的 ?id= 块）的词条
--
-- 这一块是新加的：sessions.html 第 13 行的数据说明此前只在**注释**里许了
--   Detail / DetailErr / Events / FoldPlan 四个键，模板与装配都没实现 ——
--   这类「将来」注释是设计意图的载体，实现落在哪一批、键叫什么，以它为准。
--
-- 7 个键全部同批 seed 中英：少一条，新库上英文站就显示中文兜底（不报错，只是串味）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.detail.title', 'zh-CN', '会话详情', 200, 'admin', 'admin/ai/sessions.html: 详情区标题', 1, now(), now()),
('admin.ai.detail.title', 'en-US', 'Session detail', 200, 'admin', 'admin/ai/sessions.html: 详情区标题', 1, now(), now()),
('admin.ai.detail.provider', 'zh-CN', '供应商 / 模型', 200, 'admin', 'admin/ai/sessions.html: 详情卡：供应商与模型', 1, now(), now()),
('admin.ai.detail.provider', 'en-US', 'Provider / model', 200, 'admin', 'admin/ai/sessions.html: 详情卡：供应商与模型', 1, now(), now()),
('admin.ai.detail.events', 'zh-CN', '事件条数', 200, 'admin', 'admin/ai/sessions.html: 详情卡：事件条数', 1, now(), now()),
('admin.ai.detail.events', 'en-US', 'Events', 200, 'admin', 'admin/ai/sessions.html: 详情卡：事件条数', 1, now(), now()),
('admin.ai.detail.visible', 'zh-CN', '当前上下文', 200, 'admin', 'admin/ai/sessions.html: 详情卡：当前可见上下文的 token 数', 1, now(), now()),
('admin.ai.detail.visible', 'en-US', 'Visible context', 200, 'admin', 'admin/ai/sessions.html: 详情卡：当前可见上下文的 token 数', 1, now(), now()),
('admin.ai.detail.compacts', 'zh-CN', '已折叠', 200, 'admin', 'admin/ai/sessions.html: 详情卡：已折叠次数', 1, now(), now()),
('admin.ai.detail.compacts', 'en-US', 'Compactions', 200, 'admin', 'admin/ai/sessions.html: 详情卡：已折叠次数', 1, now(), now()),
('admin.ai.detail.timeline', 'zh-CN', '事件时间线', 200, 'admin', 'admin/ai/sessions.html: 时间线小节标题', 1, now(), now()),
('admin.ai.detail.timeline', 'en-US', 'Event timeline', 200, 'admin', 'admin/ai/sessions.html: 时间线小节标题', 1, now(), now()),
('admin.ai.detail.noEvents', 'zh-CN', '这个会话还没有任何事件。', 200, 'admin', 'admin/ai/sessions.html: 空态', 1, now(), now()),
('admin.ai.detail.noEvents', 'en-US', 'This session has no events yet.', 200, 'admin', 'admin/ai/sessions.html: 空态', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
