-- 549 · 对外接入点开关（admin/ai/mcp.html）的词条
--
-- 开关本身落在 sys_config 的 'ai' 组（迁移 548），这里只有它的界面文案。
-- 8 个键全部同批 seed 中英：少一条，新库上英文站就显示中文兜底（不报错，只是串味）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.mcp.switch.on', 'zh-CN', '已开启', 200, 'admin', 'admin/ai/mcp.html: 开关状态徽标：开', 1, now(), now()),
('admin.ai.mcp.switch.on', 'en-US', 'Enabled', 200, 'admin', 'admin/ai/mcp.html: 开关状态徽标：开', 1, now(), now()),
('admin.ai.mcp.switch.off', 'zh-CN', '已关闭', 200, 'admin', 'admin/ai/mcp.html: 开关状态徽标：关', 1, now(), now()),
('admin.ai.mcp.switch.off', 'en-US', 'Disabled', 200, 'admin', 'admin/ai/mcp.html: 开关状态徽标：关', 1, now(), now()),
('admin.ai.mcp.switch.enable', 'zh-CN', '开启接入', 200, 'admin', 'admin/ai/mcp.html: 按钮：开启', 1, now(), now()),
('admin.ai.mcp.switch.enable', 'en-US', 'Enable endpoint', 200, 'admin', 'admin/ai/mcp.html: 按钮：开启', 1, now(), now()),
('admin.ai.mcp.switch.disable', 'zh-CN', '关闭接入', 200, 'admin', 'admin/ai/mcp.html: 按钮：关闭', 1, now(), now()),
('admin.ai.mcp.switch.disable', 'en-US', 'Disable endpoint', 200, 'admin', 'admin/ai/mcp.html: 按钮：关闭', 1, now(), now()),
('admin.ai.mcp.switch.confirm', 'zh-CN', '确认开启对外接入？开启后，持有令牌的外部客户端可以读取本站业务数据。', 200, 'admin', 'admin/ai/mcp.html: 开启前的二次确认（不可逆后果要说清）', 1, now(), now()),
('admin.ai.mcp.switch.confirm', 'en-US', 'Enable external access? Once enabled, external clients holding a token can read this site''s business data.', 200, 'admin', 'admin/ai/mcp.html: 开启前的二次确认（不可逆后果要说清）', 1, now(), now()),
('admin.ai.mcp.switch.hint', 'zh-CN', '默认关闭。开关改动最迟 5 秒后对请求生效；关闭后端点对任何请求都返回 404。', 200, 'admin', 'admin/ai/mcp.html: 开关说明', 1, now(), now()),
('admin.ai.mcp.switch.hint', 'en-US', 'Disabled by default. A change takes effect within 5 seconds; while disabled the endpoint returns 404 for every request.', 200, 'admin', 'admin/ai/mcp.html: 开关说明', 1, now(), now()),
('admin.ai.mcp.switch.onDone', 'zh-CN', '已开启对外接入点（最迟 5 秒后生效）', 200, 'admin', 'admin/ai/mcp.html: 开启后的回执', 1, now(), now()),
('admin.ai.mcp.switch.onDone', 'en-US', 'External endpoint enabled (effective within 5 seconds)', 200, 'admin', 'admin/ai/mcp.html: 开启后的回执', 1, now(), now()),
('admin.ai.mcp.switch.offDone', 'zh-CN', '已关闭对外接入点（最晚 5 秒后对任何请求回 404）', 200, 'admin', 'admin/ai/mcp.html: 关闭后的回执', 1, now(), now()),
('admin.ai.mcp.switch.offDone', 'en-US', 'External endpoint disabled (returns 404 within 5 seconds)', 200, 'admin', 'admin/ai/mcp.html: 关闭后的回执', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
