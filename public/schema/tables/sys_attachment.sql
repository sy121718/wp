-- table: sys_attachment（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_attachment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_attachment (
    id bigint NOT NULL,
    category_id bigint,
    file_name character varying(255) NOT NULL,
    file_path character varying(500) NOT NULL,
    file_size bigint NOT NULL,
    file_type character varying(50) NOT NULL,
    mime_type character varying(100),
    storage_type character varying(50) DEFAULT 'local'::character varying NOT NULL,
    storage_path character varying(500),
    url character varying(500),
    md5 character varying(32),
    extra_info jsonb,
    status smallint DEFAULT 1 NOT NULL,
    create_by bigint,
    update_by bigint,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    generation integer DEFAULT 1 NOT NULL
);

ALTER SEQUENCE public.sys_attachment_id_seq OWNED BY public.sys_attachment.id;

ALTER TABLE ONLY public.sys_attachment ALTER COLUMN id SET DEFAULT nextval('public.sys_attachment_id_seq'::regclass);

CREATE UNIQUE INDEX uq_attachment_md5_type_active ON public.sys_attachment USING btree (md5, file_type) WHERE ((status = 1) AND (md5 IS NOT NULL));

-- @phase constr
ALTER TABLE ONLY public.sys_attachment
    ADD CONSTRAINT sys_attachment_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_att_cat_time_alive ON public.sys_attachment USING btree (category_id, create_time DESC) WHERE (status = 1);

CREATE INDEX idx_att_create_time ON public.sys_attachment USING btree (create_time);

CREATE INDEX idx_att_extra_info_gin ON public.sys_attachment USING gin (extra_info);

CREATE INDEX idx_att_file_name_trgm ON public.sys_attachment USING gin (file_name ext_shared.gin_trgm_ops);

CREATE INDEX idx_att_file_path_alive ON public.sys_attachment USING btree (file_path) WHERE (status = 1);

CREATE INDEX idx_att_file_size ON public.sys_attachment USING btree (file_size);

CREATE INDEX idx_att_status_time ON public.sys_attachment USING btree (status, create_time);

CREATE INDEX idx_att_storage_type ON public.sys_attachment USING btree (storage_type);

CREATE INDEX idx_att_type_status ON public.sys_attachment USING btree (file_type, status);

CREATE INDEX idx_att_update_time ON public.sys_attachment USING btree (update_time);

-- @phase fk
ALTER TABLE ONLY public.sys_attachment
    ADD CONSTRAINT fk_sys_attachment_category_id FOREIGN KEY (category_id) REFERENCES public.sys_file_category(id) ON UPDATE CASCADE ON DELETE SET NULL;

-- @phase comment
COMMENT ON TABLE public.sys_attachment IS '系统附件表';

COMMENT ON COLUMN public.sys_attachment.category_id IS '分类ID';

COMMENT ON COLUMN public.sys_attachment.file_name IS '文件名';

COMMENT ON COLUMN public.sys_attachment.file_path IS '文件路径';

COMMENT ON COLUMN public.sys_attachment.file_size IS '文件大小(字节)';

COMMENT ON COLUMN public.sys_attachment.file_type IS '文件类型';

COMMENT ON COLUMN public.sys_attachment.mime_type IS 'MIME类型';

COMMENT ON COLUMN public.sys_attachment.storage_type IS '存储类型';

COMMENT ON COLUMN public.sys_attachment.storage_path IS '存储路径';

COMMENT ON COLUMN public.sys_attachment.url IS '访问URL';

COMMENT ON COLUMN public.sys_attachment.md5 IS '文件MD5（上传去重键，与 file_type 联合查重）';

COMMENT ON COLUMN public.sys_attachment.extra_info IS '额外信息（JSONB）：alt/title/description + refs 引用缓存数组';

COMMENT ON COLUMN public.sys_attachment.status IS '状态 0=禁用,1=启用';

COMMENT ON COLUMN public.sys_attachment.create_by IS '创建人ID';

COMMENT ON COLUMN public.sys_attachment.update_by IS '更新人ID';

COMMENT ON COLUMN public.sys_attachment.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_attachment.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_attachment.generation IS '换图代数，初始 1，每次换图 +1（触发依赖重建）';

COMMENT ON INDEX public.idx_att_cat_time_alive IS '媒体库按分类分页列表（status = 1 AND category_id = ? ORDER BY create_time DESC），部分索引 status = 1';

COMMENT ON INDEX public.idx_att_file_path_alive IS '构建期按 file_path 反查启用附件（media_ref.go attachmentIDByURL），部分索引 status = 1';

COMMENT ON INDEX public.sys_attachment_pkey IS '附件主键（稳定引用标识，URL 形如 /storage/<id>.<ext>）';
