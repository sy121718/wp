-- table: contents（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.contents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    entity_type text NOT NULL,
    slug text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT contents_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT contents_updated_at_not_null NOT NULL,
    title text GENERATED ALWAYS AS ((data ->> 'title'::text)) STORED,
    CONSTRAINT contents_entity_type_check CHECK ((entity_type = 'article'::text))
);

CREATE UNIQUE INDEX uq_contents_type_slug ON public.contents USING btree (entity_type, slug);

-- @phase constr
ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_contents_title_trgm ON public.contents USING gin (title ext_shared.gin_trgm_ops);

CREATE INDEX idx_contents_type_updated ON public.contents USING btree (entity_type, update_time DESC, id DESC);

-- @phase comment
COMMENT ON TABLE public.contents IS 'CMS 内容实体（0-A2 content 模块，docs/02-domain.md）';

COMMENT ON COLUMN public.contents.title IS '由 data->>''title'' 生成的查询列（审计 DB-024）：只读，供检索与排序用；写入仍只写 data';
