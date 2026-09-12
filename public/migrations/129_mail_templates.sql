-- 129 · 内置事务邮件模板（issue #37）：注册验证 / 欢迎 / 密码重置。
--
-- 模板放在迁移里 seed，而不是只留一个空列表让运维自己填：这三套是**注册流程的必经环节**，
-- 缺了它们用户就注册不了。template_key 是稳定标识，改文案不动代码。
--
-- 变量约定（发送方必须提供，否则渲染按 missingkey=error 直接报错，不会发出 <no value>）：
--   register_verify : name / code / expire_minutes
--   welcome         : name / site_name
--   password_reset  : name / code / expire_minutes
--
-- ON CONFLICT DO NOTHING：已存在的模板（运维改过文案）不被覆盖。

INSERT INTO mail_templates (template_key, locale, name, subject, body_html, body_text, variables, status, create_time)
VALUES
('register_verify', '', '注册邮箱验证',
 '验证你的邮箱',
 '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px">'
 '<h2>验证你的邮箱</h2>'
 '<p>{{.name}}，你好：</p>'
 '<p>你的验证码是：</p>'
 '<p style="font-size:28px;font-weight:700;letter-spacing:4px;margin:16px 0">{{.code}}</p>'
 '<p>验证码 {{.expire_minutes}} 分钟内有效。如果这不是你本人的操作，忽略本邮件即可。</p>'
 '<p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p>'
 '</div>',
 '{{.name}}，你好：\n\n你的验证码是：{{.code}}\n\n验证码 {{.expire_minutes}} 分钟内有效。如果这不是你本人的操作，忽略本邮件即可。\n\n本邮件由系统自动发送，请勿直接回复。',
 ARRAY['name','code','expire_minutes'], 1, NOW()),
('welcome', '', '注册欢迎',
 '欢迎加入 {{.site_name}}',
 '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px">'
 '<h2>欢迎加入 {{.site_name}}</h2>'
 '<p>{{.name}}，你好：</p>'
 '<p>你的账号已经创建好了。之后可以用注册邮箱或用户名登录。</p>'
 '<p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p>'
 '</div>',
 '{{.name}}，你好：\n\n欢迎加入 {{.site_name}}。\n你的账号已经创建好了，之后可以用注册邮箱或用户名登录。\n\n本邮件由系统自动发送，请勿直接回复。',
 ARRAY['name','site_name'], 1, NOW()),
('password_reset', '', '密码重置',
 '重置你的密码',
 '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px">'
 '<h2>重置密码</h2>'
 '<p>{{.name}}，你好：</p>'
 '<p>你的密码重置验证码是：</p>'
 '<p style="font-size:28px;font-weight:700;letter-spacing:4px;margin:16px 0">{{.code}}</p>'
 '<p>验证码 {{.expire_minutes}} 分钟内有效。如果不是你本人申请，请忽略本邮件 —— 你的密码不会被改动。</p>'
 '<p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p>'
 '</div>',
 '{{.name}}，你好：\n\n你的密码重置验证码是：{{.code}}\n\n验证码 {{.expire_minutes}} 分钟内有效。如果不是你本人申请，请忽略本邮件 —— 你的密码不会被改动。\n\n本邮件由系统自动发送，请勿直接回复。',
 ARRAY['name','code','expire_minutes'], 1, NOW())
ON CONFLICT (template_key, locale) DO NOTHING;
