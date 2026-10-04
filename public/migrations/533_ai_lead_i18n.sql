-- 533 · 统一入口的说明文案（admin/ai/sessions.html 页头 ? 提示）。
--
-- 背景：页面合并成「大模型管理」之后（532），页头那句说明还是只讲会话的旧文案，
--   与现在的两个标签对不上。533 补一条新的，旧 key 保留（历史审计与已发布产物可能引用）。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.lead', 'zh-CN', '两个标签：AI 模型配置供应商与模型目录；AI 会话是每个会话的用量记录 —— 谁在什么时候消耗了多少 token。', 200, 'admin', 'admin/ai/sessions.html: 页头说明', 1, now(), now()),
('admin.ai.lead', 'en-US', 'Two tabs: AI Models configures providers and their model catalog; AI Sessions is the usage record — who spent how many tokens and when.', 200, 'admin', 'admin/ai/sessions.html: page lead', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
