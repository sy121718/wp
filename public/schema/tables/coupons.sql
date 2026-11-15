-- table: coupons（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.coupons_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.coupons (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    code character varying(64) NOT NULL,
    name character varying(120) DEFAULT ''::character varying NOT NULL,
    discount_type character varying(16) NOT NULL,
    discount_value bigint NOT NULL,
    min_subtotal bigint DEFAULT 0 NOT NULL,
    max_uses integer DEFAULT 0 NOT NULL,
    used_count integer DEFAULT 0 NOT NULL,
    per_user_limit integer DEFAULT 0 NOT NULL,
    starts_at timestamp with time zone,
    ends_at timestamp with time zone,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(255) DEFAULT ''::character varying NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_coupons_discount_type CHECK (((discount_type)::text = ANY ((ARRAY['percent'::character varying, 'fixed'::character varying])::text[]))),
    CONSTRAINT ck_coupons_discount_value CHECK (((((discount_type)::text = 'percent'::text) AND ((discount_value >= 1) AND (discount_value <= 100))) OR (((discount_type)::text = 'fixed'::text) AND (discount_value > 0)))),
    CONSTRAINT ck_coupons_limits CHECK (((max_uses >= 0) AND (per_user_limit >= 0) AND (used_count >= 0))),
    CONSTRAINT ck_coupons_min_subtotal CHECK ((min_subtotal >= 0)),
    CONSTRAINT ck_coupons_status CHECK ((status = ANY (ARRAY[0, 1]))),
    CONSTRAINT ck_coupons_window CHECK (((starts_at IS NULL) OR (ends_at IS NULL) OR (ends_at > starts_at)))
);

ALTER TABLE ONLY public.coupons FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.coupons_id_seq OWNED BY public.coupons.id;

ALTER TABLE ONLY public.coupons ALTER COLUMN id SET DEFAULT nextval('public.coupons_id_seq'::regclass);

CREATE UNIQUE INDEX uq_coupons_project_code ON public.coupons USING btree (project_id, upper((code)::text));

ALTER TABLE public.coupons ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.coupons USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.coupons
    ADD CONSTRAINT coupons_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_coupons_project_status ON public.coupons USING btree (project_id, status);

-- @phase comment
COMMENT ON COLUMN public.coupons.starts_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.coupons.ends_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.coupons.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.coupons.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
