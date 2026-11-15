-- table: sys_admin（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_admin_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_admin (
    id bigint NOT NULL,
    dept_id bigint DEFAULT 0 NOT NULL,
    username character varying(50) NOT NULL,
    password character varying(100) NOT NULL,
    name character varying(50),
    avatar character varying(255),
    email character varying(100),
    phone character varying(20),
    status smallint DEFAULT 1 NOT NULL,
    is_admin smallint DEFAULT 0 NOT NULL,
    login_failure_count integer DEFAULT 0 NOT NULL,
    locked_until_time timestamp with time zone,
    metadata jsonb,
    last_failure_time timestamp with time zone,
    register_ip character varying(50),
    register_location character varying(100),
    last_login_ip character varying(50),
    last_login_location character varying(100),
    last_login_isp character varying(50),
    last_login_time timestamp with time zone,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone,
    remark character varying(255)
);

ALTER SEQUENCE public.sys_admin_id_seq OWNED BY public.sys_admin.id;

ALTER TABLE ONLY public.sys_admin ALTER COLUMN id SET DEFAULT nextval('public.sys_admin_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_admin
    ADD CONSTRAINT sys_admin_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.sys_admin
    ADD CONSTRAINT uk_sys_admin_username UNIQUE (username);

-- @phase idx
CREATE INDEX idx_sys_admin_email ON public.sys_admin USING btree (email);

CREATE INDEX idx_sys_admin_phone ON public.sys_admin USING btree (phone);

CREATE INDEX idx_sys_admin_status ON public.sys_admin USING btree (status);

-- @phase comment
COMMENT ON TABLE public.sys_admin IS '管理员表';

COMMENT ON COLUMN public.sys_admin.dept_id IS '所属部门ID';

COMMENT ON COLUMN public.sys_admin.username IS '登录账号';

COMMENT ON COLUMN public.sys_admin.password IS '加密密码';

COMMENT ON COLUMN public.sys_admin.status IS '状态：1启用 2禁用 3封禁';

COMMENT ON COLUMN public.sys_admin.is_admin IS '是否超级管理员：0否 1是';

COMMENT ON COLUMN public.sys_admin.login_failure_count IS '连续登录失败次数';

COMMENT ON COLUMN public.sys_admin.locked_until_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_admin.metadata IS '扩展元数据';

COMMENT ON COLUMN public.sys_admin.last_failure_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_admin.last_login_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_admin.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_admin.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
INSERT INTO public.sys_admin VALUES
	(1, 0, 'admin', '$2a$10$nVJqJvlNvbZqF26X/un.huZEhsw8.Dyv0GvH/9XfFu6DhOfQ3YDc2', '超级管理员', NULL, NULL, NULL, 1, 1, 0, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 0, '2026-11-15 20:36:55.155722+08', 0, '2026-11-15 20:36:55.155722+08', NULL) ON CONFLICT DO NOTHING;

SELECT pg_catalog.setval('public.sys_admin_id_seq', 1, true);
