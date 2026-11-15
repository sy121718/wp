-- table: page_views（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.page_views_id_seq1
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.page_views (
    id bigint CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
)
PARTITION BY RANGE (viewed_at);

ALTER TABLE ONLY public.page_views FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.page_views_id_seq1 OWNED BY public.page_views.id;

ALTER TABLE ONLY public.page_views ALTER COLUMN id SET DEFAULT nextval('public.page_views_id_seq1'::regclass);

ALTER TABLE public.page_views ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.page_views USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.page_views
    ADD CONSTRAINT page_views_pkey1 PRIMARY KEY (id, viewed_at);

-- @phase idx
CREATE INDEX idx_page_views_project_path_time ON ONLY public.page_views USING btree (project_id, path, viewed_at DESC);

CREATE INDEX idx_page_views_project_time ON ONLY public.page_views USING btree (project_id, viewed_at DESC);

-- @phase comment
COMMENT ON TABLE public.page_views IS '页面浏览明细（BIZ-8；按月分区，审计 DB-004）：分区按 viewed_at，DEFAULT 分区兜底缺窗口';
