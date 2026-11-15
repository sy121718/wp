-- table: mail_templates（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.mail_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.mail_templates (
    id bigint NOT NULL,
    template_key character varying(64) NOT NULL,
    locale character varying(16) DEFAULT ''::character varying NOT NULL,
    name character varying(100) NOT NULL,
    subject character varying(255) NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_text text DEFAULT ''::text NOT NULL,
    variables text[] DEFAULT '{}'::text[] NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    body_json jsonb,
    body_compiled_html text DEFAULT ''::text NOT NULL,
    body_compiled_text text DEFAULT ''::text NOT NULL
);

ALTER SEQUENCE public.mail_templates_id_seq OWNED BY public.mail_templates.id;

ALTER TABLE ONLY public.mail_templates ALTER COLUMN id SET DEFAULT nextval('public.mail_templates_id_seq'::regclass);

CREATE UNIQUE INDEX idx_mail_templates_key_locale ON public.mail_templates USING btree (template_key, locale);

-- @phase constr
ALTER TABLE ONLY public.mail_templates
    ADD CONSTRAINT mail_templates_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON COLUMN public.mail_templates.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.mail_templates.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
INSERT INTO public.mail_templates VALUES
	(1, 'register_verify', '', '注册邮箱验证', '验证你的邮箱', '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px"><h2>验证你的邮箱</h2><p>{{.name}}，你好：</p><p>你的验证码是：</p><p style="font-size:28px;font-weight:700;letter-spacing:4px;margin:16px 0">{{.code}}</p><p>验证码 {{.expire_minutes}} 分钟内有效。如果这不是你本人的操作，忽略本邮件即可。</p><p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p></div>', '{{.name}}，你好：\n\n你的验证码是：{{.code}}\n\n验证码 {{.expire_minutes}} 分钟内有效。如果这不是你本人的操作，忽略本邮件即可。\n\n本邮件由系统自动发送，请勿直接回复。', '{name,code,expire_minutes}', 1, '2026-11-15 20:36:55.246825+08', NULL, NULL, '', ''),
	(2, 'welcome', '', '注册欢迎', '欢迎加入 {{.site_name}}', '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px"><h2>欢迎加入 {{.site_name}}</h2><p>{{.name}}，你好：</p><p>你的账号已经创建好了。之后可以用注册邮箱或用户名登录。</p><p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p></div>', '{{.name}}，你好：\n\n欢迎加入 {{.site_name}}。\n你的账号已经创建好了，之后可以用注册邮箱或用户名登录。\n\n本邮件由系统自动发送，请勿直接回复。', '{name,site_name}', 1, '2026-11-15 20:36:55.246825+08', NULL, NULL, '', ''),
	(3, 'password_reset', '', '密码重置', '重置你的密码', '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px"><h2>重置密码</h2><p>{{.name}}，你好：</p><p>你的密码重置验证码是：</p><p style="font-size:28px;font-weight:700;letter-spacing:4px;margin:16px 0">{{.code}}</p><p>验证码 {{.expire_minutes}} 分钟内有效。如果不是你本人申请，请忽略本邮件 —— 你的密码不会被改动。</p><p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p></div>', '{{.name}}，你好：\n\n你的密码重置验证码是：{{.code}}\n\n验证码 {{.expire_minutes}} 分钟内有效。如果不是你本人申请，请忽略本邮件 —— 你的密码不会被改动。\n\n本邮件由系统自动发送，请勿直接回复。', '{name,code,expire_minutes}', 1, '2026-11-15 20:36:55.246825+08', NULL, NULL, '', ''),
	(4, 'guest_account', '', '访客下单初始密码', '你的账号已创建', '<div style="font-family:system-ui,-apple-system,Segoe UI,sans-serif;line-height:1.7;max-width:560px"><h2>你的账号已创建</h2><p>{{.name}}，你好：</p><p>你在 {{.site_name}} 下单时，我们为你自动创建了账号，之后可以登录查看订单。</p><p>用户名：<b>{{.username}}</b></p><p>初始密码：<b style="font-size:18px;letter-spacing:2px">{{.password}}</b></p><p>为了账号安全，请登录后尽快修改密码。这个密码只发给你本人，我们不会以任何方式另行索取。</p><p style="color:#888;font-size:13px">本邮件由系统自动发送，请勿直接回复。</p></div>', '{{.name}}，你好：

你在 {{.site_name}} 下单时，我们为你自动创建了账号，之后可以登录查看订单。

用户名：{{.username}}
初始密码：{{.password}}

为了账号安全，请登录后尽快修改密码。这个密码只发给你本人，我们不会以任何方式另行索取。

本邮件由系统自动发送，请勿直接回复。', '{name,username,password,site_name}', 1, '2026-11-15 20:36:55.262406+08', NULL, NULL, '', '') ON CONFLICT DO NOTHING;

SELECT pg_catalog.setval('public.mail_templates_id_seq', 4, true);
