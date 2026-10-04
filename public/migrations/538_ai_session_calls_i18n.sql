-- 538 · 会话行悬浮卡「最近调用」的词条（admin.ai.session.calls.*）。
--
-- 背景：537 建了 ai_call_log，但那张表只写不读 —— 用户要的「每个会话的每个请求的数据」
--   在页面上看不到。538 起悬浮卡在「按供应商/模型聚合」下面再显示一段流水，
--   词条就是这一段的表头与状态。
--
-- 复用的既有词条：admin.ai.session.col.model（模型）。其余几列各自新建，
--   因为「上下文 token」（聚合列）与「Token」（单次调用列）不是一回事，共用一条会让
--   以后改文案时被迫两处一起改。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.session.calls.title', 'zh-CN', '最近调用', 200, 'admin', 'admin/ai/sessions.html: 悬浮卡流水段标题', 1, now(), now()),
('admin.ai.session.calls.title', 'en-US', 'Recent calls', 200, 'admin', 'admin/ai/sessions.html: hover card calls section title', 1, now(), now()),
('admin.ai.session.calls.total', 'zh-CN', '共', 200, 'admin', 'admin/ai/sessions.html: 调用总条数前缀', 1, now(), now()),
('admin.ai.session.calls.total', 'en-US', 'Total', 200, 'admin', 'admin/ai/sessions.html: total calls prefix', 1, now(), now()),
('admin.ai.session.calls.unit', 'zh-CN', '次调用', 200, 'admin', 'admin/ai/sessions.html: 调用总条数后缀', 1, now(), now()),
('admin.ai.session.calls.unit', 'en-US', 'calls', 200, 'admin', 'admin/ai/sessions.html: total calls suffix', 1, now(), now()),
('admin.ai.session.calls.col.time', 'zh-CN', '时间', 200, 'admin', 'admin/ai/sessions.html: 调用流水列头', 1, now(), now()),
('admin.ai.session.calls.col.time', 'en-US', 'Time', 200, 'admin', 'admin/ai/sessions.html: calls column header', 1, now(), now()),
('admin.ai.session.calls.col.latency', 'zh-CN', '耗时', 200, 'admin', 'admin/ai/sessions.html: 调用流水列头', 1, now(), now()),
('admin.ai.session.calls.col.latency', 'en-US', 'Latency', 200, 'admin', 'admin/ai/sessions.html: calls column header', 1, now(), now()),
('admin.ai.session.calls.col.tokens', 'zh-CN', 'Token', 200, 'admin', 'admin/ai/sessions.html: 调用流水列头（单次调用用量）', 1, now(), now()),
('admin.ai.session.calls.col.tokens', 'en-US', 'Tokens', 200, 'admin', 'admin/ai/sessions.html: calls column header (per-call usage)', 1, now(), now()),
('admin.ai.session.calls.col.status', 'zh-CN', '状态', 200, 'admin', 'admin/ai/sessions.html: 调用流水列头', 1, now(), now()),
('admin.ai.session.calls.col.status', 'en-US', 'Status', 200, 'admin', 'admin/ai/sessions.html: calls column header', 1, now(), now()),
('admin.ai.session.calls.ok', 'zh-CN', '成功', 200, 'admin', 'admin/ai/sessions.html: 调用成功徽标', 1, now(), now()),
('admin.ai.session.calls.ok', 'en-US', 'OK', 200, 'admin', 'admin/ai/sessions.html: call ok badge', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
