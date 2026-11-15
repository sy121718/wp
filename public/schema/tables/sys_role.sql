-- table: sys_role（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_role_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_role (
    id bigint NOT NULL,
    role_code character varying(50) NOT NULL,
    role_name character varying(100) NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    is_system smallint DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.sys_role_id_seq OWNED BY public.sys_role.id;

ALTER TABLE ONLY public.sys_role ALTER COLUMN id SET DEFAULT nextval('public.sys_role_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_role
    ADD CONSTRAINT sys_role_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.sys_role
    ADD CONSTRAINT uk_sys_role_code UNIQUE (role_code);

-- @phase comment
COMMENT ON TABLE public.sys_role IS '角色表';

COMMENT ON COLUMN public.sys_role.role_code IS '角色编码';

COMMENT ON COLUMN public.sys_role.role_name IS '角色名称';

COMMENT ON COLUMN public.sys_role.status IS '状态：0禁用 1启用';

COMMENT ON COLUMN public.sys_role.is_system IS '是否系统内置：0否 1是';

COMMENT ON COLUMN public.sys_role.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_role.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.sys_role_id_seq', 1, false);
