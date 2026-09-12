-- 137 · 访客下单自动开号的「初始密码」邮件模板。
--
-- 与 129 同构：template_key + locale 是稳定标识，运维改过的文案不被覆盖。
--
-- 变量必须与 user/service/user_guest_account.go 里 sendGuestAccountMail 的 Vars 逐字一致 ——
-- 两边对不上时模板会把这些位置渲染成空值而**不报错**，是这类问题里最难发现的一种。

INSERT INTO mail_templates (template_key, locale, name, subject, body_html, body_text, variables, status, create_time)
VALUES
('guest_account', '', '访客下单初始密码',
 '你的账号已创建',
 '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px">'
 '<h2>你的账号已创建</h2>'
 '<p>{{.name}}，你好：</p>'
 '<p>你在 {{.site_name}} 下单时，我们为你自动创建了账号，之后可以登录查看订单。</p>'
 '<p>用户名：<b>{{.username}}</b></p>'
 '<p>初始密码：<b style="font-size:18px;letter-spacing:2px">{{.password}}</b></p>'
 '<p>为了账号安全，请登录后尽快修改密码。这个密码只发给你本人，我们不会以任何方式另行索取。</p>'
 '<p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p>'
 '</div>',
 '{{.name}}，你好：' || chr(10) || chr(10) ||
 '你在 {{.site_name}} 下单时，我们为你自动创建了账号，之后可以登录查看订单。' || chr(10) || chr(10) ||
 '用户名：{{.username}}' || chr(10) ||
 '初始密码：{{.password}}' || chr(10) || chr(10) ||
 '为了账号安全，请登录后尽快修改密码。这个密码只发给你本人，我们不会以任何方式另行索取。' || chr(10) || chr(10) ||
 '本邮件由系统自动发送，请勿直接回复。',
 ARRAY['name','username','password','site_name'], 1, NOW())
ON CONFLICT (template_key, locale) DO NOTHING;
