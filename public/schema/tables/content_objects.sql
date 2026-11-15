-- table: content_objects（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.content_objects (
    content_hash text NOT NULL,
    provider text NOT NULL,
    object_key text NOT NULL,
    byte_size bigint NOT NULL,
    create_time timestamp with time zone CONSTRAINT content_objects_created_at_not_null NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT content_objects_byte_size_check CHECK ((byte_size >= 0))
);

-- @phase constr
ALTER TABLE ONLY public.content_objects
    ADD CONSTRAINT content_objects_pkey PRIMARY KEY (content_hash);

ALTER TABLE ONLY public.content_objects
    ADD CONSTRAINT content_objects_provider_object_key_key UNIQUE (provider, object_key);
