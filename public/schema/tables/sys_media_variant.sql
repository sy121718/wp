-- table: sys_media_variant（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_media_variant_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_media_variant (
    id bigint NOT NULL,
    attachment_id bigint NOT NULL,
    variant_type character varying(20) NOT NULL,
    file_path character varying(500) NOT NULL,
    width integer,
    height integer,
    file_size bigint DEFAULT 0 NOT NULL,
    mime_type character varying(100),
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.sys_media_variant_id_seq OWNED BY public.sys_media_variant.id;

ALTER TABLE ONLY public.sys_media_variant ALTER COLUMN id SET DEFAULT nextval('public.sys_media_variant_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_media_variant
    ADD CONSTRAINT sys_media_variant_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.sys_media_variant
    ADD CONSTRAINT uq_media_variant_attachment_type UNIQUE (attachment_id, variant_type);

-- @phase idx
CREATE INDEX idx_mva_attachment ON public.sys_media_variant USING btree (attachment_id);

CREATE INDEX idx_mva_file_path ON public.sys_media_variant USING btree (file_path);

CREATE INDEX idx_mva_status ON public.sys_media_variant USING btree (status);

-- @phase fk
ALTER TABLE ONLY public.sys_media_variant
    ADD CONSTRAINT fk_media_variant_attachment FOREIGN KEY (attachment_id) REFERENCES public.sys_attachment(id) ON UPDATE CASCADE ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.sys_media_variant IS '媒体图片变体表（缩略图/调节尺寸版/全尺寸webp）';

COMMENT ON COLUMN public.sys_media_variant.attachment_id IS '所属附件ID（物理级联删除）';

COMMENT ON COLUMN public.sys_media_variant.variant_type IS '变体类型：thumb/medium/webp';

COMMENT ON COLUMN public.sys_media_variant.file_path IS '变体文件存储相对路径（local storage key）';

COMMENT ON COLUMN public.sys_media_variant.width IS '变体宽度（像素）';

COMMENT ON COLUMN public.sys_media_variant.height IS '变体高度（像素）';

COMMENT ON COLUMN public.sys_media_variant.file_size IS '变体文件大小（字节）';

COMMENT ON COLUMN public.sys_media_variant.mime_type IS '变体 MIME 类型';

COMMENT ON COLUMN public.sys_media_variant.status IS '生成状态：pending/processing/ready/failed';

COMMENT ON COLUMN public.sys_media_variant.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_media_variant.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON INDEX public.idx_mva_file_path IS '构建期按 file_path 反查媒体变体（media_variant_model.go GetByFilePath）';

-- @phase data
SELECT pg_catalog.setval('public.sys_media_variant_id_seq', 1, false);
