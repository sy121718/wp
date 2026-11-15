-- table: sys_dept（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_dept_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_dept (
    id bigint NOT NULL,
    parent_id bigint DEFAULT 0 NOT NULL,
    ancestors character varying(500) DEFAULT ''::character varying NOT NULL,
    dept_name character varying(100) NOT NULL,
    dept_code character varying(50) NOT NULL,
    leader_id bigint,
    sort_order integer DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.sys_dept_id_seq OWNED BY public.sys_dept.id;

ALTER TABLE ONLY public.sys_dept ALTER COLUMN id SET DEFAULT nextval('public.sys_dept_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_dept
    ADD CONSTRAINT sys_dept_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.sys_dept
    ADD CONSTRAINT uk_sys_dept_code UNIQUE (dept_code);

-- @phase comment
COMMENT ON TABLE public.sys_dept IS '部门表';

COMMENT ON COLUMN public.sys_dept.ancestors IS '祖先ID链，如 ,0,1,2,';

COMMENT ON COLUMN public.sys_dept.leader_id IS '部门负责人ID';

COMMENT ON COLUMN public.sys_dept.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_dept.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.sys_dept_id_seq', 1, false);
