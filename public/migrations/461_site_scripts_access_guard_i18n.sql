-- 461 · 站点自定义注入代码（PIPE-8）与访问守卫面板（PIPE-6）的词条。
--
-- 背景：这两批为守「零迁移」的边界，校验提示与面板文案一律走调用点写的中文兜底
-- （pkg/i18n 的第 3 级兜底链），所以界面不显示裸 key、门禁也一直是 0 行未 key 化 ——
-- 但英文界面下这些文案会回落成中文。本迁移把这两批的 19 个 key 一次补齐（19 × 2 = 38 行）。
--
-- 取值来源（真源，不是模板里现编的字面量）：
--   · `admin.settings.scripts.head_invalid` / `.body_invalid` —— 真源是
--     internal/module/project/enums/site_scripts_keys.go 的两个常量值，经
--     internal/templates/admin/project/settings.html 的 .["t"](key, 中文兜底) **就地渲染**
--     （刻意不进 ?err= 通道：那会让几百字节的自定义代码随回跳表单一起丢）；
--   · `admin.settings.field|hint|ph.head_scripts|body_scripts` —— 真源是
--     internal/templates/admin/project/settings.html 的兜底原文；
--   · `workbench.ui.settings.access*` —— 真源是
--     internal/templates/fragments/settings_panel.html 的兜底原文。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。门槛判据在
-- register_site_scripts_access_i18n.go 里**逐条枚举本批 19 个 item_key**（上界封闭，38 行）：
-- 不用 LIKE 前缀（别的批次已有同前缀行时计数虚高 → 本批被静默跳过，058 的真实故障），
-- 也不用全库总量（将来新增同前缀 key 会永远追不平 → 每次启动重跑，076 的真实故障）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— PIPE-8：自定义 Head / Body 代码校验失败（就地提示） ——
('admin.settings.scripts.head_invalid', 'zh-CN', '自定义 Head 代码不合法：不能包含 <!DOCTYPE> / <html> / <head> / <body> 这类结构性标签，且不超过 16 KiB', 400, 'error', 'internal/module/project/enums/site_scripts_keys.go', 1, now(), now()),
('admin.settings.scripts.head_invalid', 'en-US', 'Invalid custom Head code: it must not contain structural tags such as <!DOCTYPE> / <html> / <head> / <body>, and it must stay under 16 KiB.', 400, 'error', 'internal/module/project/enums/site_scripts_keys.go', 1, now(), now()),
('admin.settings.scripts.body_invalid', 'zh-CN', '自定义 Body 代码不合法：不能包含 <!DOCTYPE> / <html> / <head> / <body> 这类结构性标签，且不超过 16 KiB', 400, 'error', 'internal/module/project/enums/site_scripts_keys.go', 1, now(), now()),
('admin.settings.scripts.body_invalid', 'en-US', 'Invalid custom Body code: it must not contain structural tags such as <!DOCTYPE> / <html> / <head> / <body>, and it must stay under 16 KiB.', 400, 'error', 'internal/module/project/enums/site_scripts_keys.go', 1, now(), now()),

-- —— PIPE-8：站点设置表单的字段名 / 提示 / 占位 ——
('admin.settings.field.head_scripts', 'zh-CN', '自定义 Head 代码（高级）', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.head_scripts', 'en-US', 'Custom Head code (advanced)', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.head_scripts', 'zh-CN', '整段粘贴第三方统计 / 营销代码（GA4、Meta Pixel、Clarity、Hotjar 等），发布时注入所有页面 </head> 之前、独占一行。保存后', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.head_scripts', 'en-US', 'Paste third-party analytics or marketing snippets (GA4, Meta Pixel, Clarity, Hotjar, ...) as one block; at publish time it is injected on its own line before </head> of every page. After saving,', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.head_scripts.republish', 'zh-CN', '需要重新发布页面', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.head_scripts.republish', 'en-US', 'pages must be republished', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.head_scripts.tail', 'zh-CN', '才会写进产物。留空或清空即不注入：产物里一个字节都不会多。这里的内容会<strong>原样</strong>进入对外页面（不转义、不改写），等于把脚本执行权交给它的来源 —— 请只填可信来源，并注意这些脚本能读到访客在本站的数据。保存时拒绝整页 HTML（含 <html> / <head> / <body> 标签）与超过 16 KiB 的内容。', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.head_scripts.tail', 'en-US', 'for it to reach the published artifacts. Leave it empty to inject nothing — not a single byte is added. Whatever you put here goes into public pages <strong>verbatim</strong> (no escaping, no rewriting), which hands script execution power to its source: only use sources you trust, and note that such scripts can read visitor data on this site. Saving rejects full HTML documents (containing <html> / <head> / <body>) and anything over 16 KiB.', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.head_scripts', 'zh-CN', '留空即不注入。例：&lt;script src=&#34;https://cdn.example.com/tag.js&#34; async&gt;&lt;/script&gt;', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.head_scripts', 'en-US', 'Empty means nothing is injected. Example: &lt;script src=&#34;https://cdn.example.com/tag.js&#34; async&gt;&lt;/script&gt;', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.body_scripts', 'zh-CN', '自定义 Body 代码（高级）', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.body_scripts', 'en-US', 'Custom Body code (advanced)', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.body_scripts', 'zh-CN', '整段粘贴需要在正文之后加载的第三方代码（在线客服浮窗、转化追踪等），发布时注入所有页面 </body> 之前、独占一行（排在站点自己的脚本之后）。判据与 Head 代码完全相同：留空即不注入，拒绝整页 HTML 与超过 16 KiB 的内容。', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.body_scripts', 'en-US', 'Paste third-party code that must load after the body content (live-chat widgets, conversion tracking, ...); at publish time it is injected on its own line before </body> of every page (after the site own scripts). The rules match Head code exactly: empty means nothing is injected, and full HTML documents or anything over 16 KiB are rejected.', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.body_scripts', 'zh-CN', '留空即不注入。例：&lt;script src=&#34;https://cdn.example.com/chat.js&#34; async&gt;&lt;/script&gt;', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.body_scripts', 'en-US', 'Empty means nothing is injected. Example: &lt;script src=&#34;https://cdn.example.com/chat.js&#34; async&gt;&lt;/script&gt;', 200, 'ui', 'internal/templates/admin/project/settings.html', 1, now(), now()),

-- —— PIPE-6：工作台「访问权限」面板（fragments/settings_panel.html） ——
('workbench.ui.settings.access', 'zh-CN', '访问权限', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.access', 'en-US', 'Access', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPublic', 'zh-CN', '公开', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPublic', 'en-US', 'Public', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPassword', 'zh-CN', '密码', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPassword', 'en-US', 'Password', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessMembers', 'zh-CN', '登录可见', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessMembers', 'en-US', 'Signed-in visitors only', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordLabel', 'zh-CN', '访问密码', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordLabel', 'en-US', 'Access password', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordSet', 'zh-CN', '已设置', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordSet', 'en-US', 'Set', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordUnset', 'zh-CN', '未设置', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordUnset', 'en-US', 'Not set', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordPlaceholder', 'zh-CN', '输入新密码后点「设置」', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessPasswordPlaceholder', 'en-US', 'Type a new password, then click Set', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessApply', 'zh-CN', '设置', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now()),
('workbench.ui.settings.accessApply', 'en-US', 'Set', 200, 'ui', 'internal/templates/fragments/settings_panel.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
