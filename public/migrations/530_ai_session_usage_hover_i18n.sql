-- 530 · 列表行 token 悬浮卡（admin/ai/sessions.html）的两个词条。
--
-- 背景：token 单元格改成「悬停看按供应商/模型的拆分」后新增两个 key。
--   528 已执行过（代表 key 已满足），按「已上线的迁移不改」另开一条。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的 key（register_ai_session_usage_hover_i18n.go）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.session.usage.breakdown', 'zh-CN', 'Token 消耗（按供应商 / 模型）', 200, 'admin', 'admin/ai/sessions.html: 悬浮卡标题', 1, now(), now()),
('admin.ai.session.usage.breakdown', 'en-US', 'Token usage by provider / model', 200, 'admin', 'admin/ai/sessions.html: hovercard title', 1, now(), now()),
('admin.ai.session.usage.unrecorded', 'zh-CN', '未记录', 200, 'admin', 'admin/ai/sessions.html: 529 之前的历史事件', 1, now(), now()),
('admin.ai.session.usage.unrecorded', 'en-US', 'Not recorded', 200, 'admin', 'admin/ai/sessions.html: events before 529', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
