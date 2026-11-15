-- table: sys_file_category（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_file_category_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_file_category (
    id bigint NOT NULL,
    category_name character varying(100) NOT NULL,
    category_code character varying(50) NOT NULL,
    parent_id bigint DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    icon character varying(50),
    status smallint DEFAULT 1 NOT NULL,
    create_by bigint,
    update_by bigint,
    create_time timestamp with time zone,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.sys_file_category_id_seq OWNED BY public.sys_file_category.id;

ALTER TABLE ONLY public.sys_file_category ALTER COLUMN id SET DEFAULT nextval('public.sys_file_category_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_file_category
    ADD CONSTRAINT sys_file_category_category_code_unique UNIQUE (category_code);

ALTER TABLE ONLY public.sys_file_category
    ADD CONSTRAINT sys_file_category_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_fc_create_by ON public.sys_file_category USING btree (create_by);

CREATE INDEX idx_fc_parent_id ON public.sys_file_category USING btree (parent_id);

CREATE INDEX idx_fc_status ON public.sys_file_category USING btree (status);

CREATE INDEX idx_fc_update_by ON public.sys_file_category USING btree (update_by);

-- @phase comment
COMMENT ON TABLE public.sys_file_category IS '文件分类表';

COMMENT ON COLUMN public.sys_file_category.category_name IS '分类名称';

COMMENT ON COLUMN public.sys_file_category.category_code IS '分类编码';

COMMENT ON COLUMN public.sys_file_category.parent_id IS '父级ID';

COMMENT ON COLUMN public.sys_file_category.sort_order IS '排序';

COMMENT ON COLUMN public.sys_file_category.icon IS '图标';

COMMENT ON COLUMN public.sys_file_category.status IS '状态';

COMMENT ON COLUMN public.sys_file_category.create_by IS '创建人';

COMMENT ON COLUMN public.sys_file_category.update_by IS '更新人';

COMMENT ON COLUMN public.sys_file_category.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_file_category.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
