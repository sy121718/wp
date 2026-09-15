-- 197 · 后台 HTMX 全局反馈 + 仪表盘数据口径标注的文案词条（审计 UI-011 / UI-012）。
--
-- 覆盖 internal/templates/admin/ 下的：
--   layout.html    —— 全局 HTMX 请求指示（shell.htmx.* 三条失败提示）
--   dashboard.html —— 数据口径标注（admin.dashboard.placeholder.* 五条）
--
-- 模板侧写法：{{ .["t"]("shell.htmx.error", "请求失败，请重试") }}，与既有批次一致；
-- 词条一律**纯文本**：admin_group_f_i18n_test.go 的兜底纯文本判据会把含 < > 的文案判失败。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('shell.htmx.error', 'zh-CN', '请求失败，请重试', 200, 'ui', 'admin/layout.html', 1, now(), now()),
    ('shell.htmx.error', 'en-US', 'Request failed, please try again', 200, 'ui', 'admin/layout.html', 1, now(), now()),
    ('shell.htmx.network', 'zh-CN', '网络异常，请求未送达', 200, 'ui', 'admin/layout.html', 1, now(), now()),
    ('shell.htmx.network', 'en-US', 'Network error, the request was not sent', 200, 'ui', 'admin/layout.html', 1, now(), now()),
    ('shell.htmx.timeout', 'zh-CN', '请求超时，请重试', 200, 'ui', 'admin/layout.html', 1, now(), now()),
    ('shell.htmx.timeout', 'en-US', 'Request timed out, please try again', 200, 'ui', 'admin/layout.html', 1, now(), now()),
    ('admin.dashboard.placeholder.lead', 'zh-CN', '演示占位：本页数值、列表与表单均为示例数据，不代表真实统计。', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.lead', 'en-US', 'Demo placeholder: the figures, list and form on this page are sample data, not live statistics.', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.links', 'zh-CN', '真实数据见：', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.links', 'en-US', 'Live data:', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.analytics', 'zh-CN', '访问统计', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.analytics', 'en-US', 'Analytics', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.orders', 'zh-CN', '订单', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.orders', 'en-US', 'Orders', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.inventory', 'zh-CN', '库存', 200, 'admin', 'admin/dashboard.html', 1, now(), now()),
    ('admin.dashboard.placeholder.inventory', 'en-US', 'Inventory', 200, 'admin', 'admin/dashboard.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
