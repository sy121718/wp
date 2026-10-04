-- 528 · AI 会话详情抽屉（admin/ai/session_detail.html）顶部 token 消耗三格的词条。
--
-- 背景：详情从「列表下方平铺」改成「按需拉取的抽屉」后新增了这组口径说明。
--   527 已经执行过（ConditionSQL 的代表 key 已满足），按「已上线的迁移不改」另开一条，
--   否则改了 527 也不会重跑 —— 这正是迁移 494 记过的静默跳过。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的 key（register_ai_session_usage_notes_i18n.go）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.session.usage.total', 'zh-CN', '累计消耗', 200, 'admin', 'admin/ai/session_detail.html: 累计消耗', 1, now(), now()),
('admin.ai.session.usage.total', 'en-US', 'Total spent', 200, 'admin', 'admin/ai/session_detail.html: total spent', 1, now(), now()),
('admin.ai.session.usage.totalNote', 'zh-CN', '含已折叠的事件正文', 200, 'admin', 'admin/ai/session_detail.html: 累计口径', 1, now(), now()),
('admin.ai.session.usage.totalNote', 'en-US', 'Includes folded event bodies', 200, 'admin', 'admin/ai/session_detail.html: total note', 1, now(), now()),
('admin.ai.session.usage.contextNote', 'zh-CN', '下一轮实际发出的量', 200, 'admin', 'admin/ai/session_detail.html: 上下文口径', 1, now(), now()),
('admin.ai.session.usage.contextNote', 'en-US', 'What the next turn actually sends', 200, 'admin', 'admin/ai/session_detail.html: context note', 1, now(), now()),
('admin.ai.session.usage.compactNote', 'zh-CN', '折叠后原事件仍保留', 200, 'admin', 'admin/ai/session_detail.html: 压缩口径', 1, now(), now()),
('admin.ai.session.usage.compactNote', 'en-US', 'Folded events are kept', 200, 'admin', 'admin/ai/session_detail.html: compact note', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
