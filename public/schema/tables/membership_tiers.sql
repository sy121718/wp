-- table: membership_tiers（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.membership_tiers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.membership_tiers (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    name character varying(60) NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    threshold_amount bigint DEFAULT 0 NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    remark character varying(200) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.membership_tiers FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.membership_tiers_id_seq OWNED BY public.membership_tiers.id;

ALTER TABLE ONLY public.membership_tiers ALTER COLUMN id SET DEFAULT nextval('public.membership_tiers_id_seq'::regclass);

CREATE UNIQUE INDEX uq_membership_tiers_default ON public.membership_tiers USING btree (project_id) WHERE (is_default AND (deleted_at IS NULL));

CREATE UNIQUE INDEX uq_membership_tiers_name ON public.membership_tiers USING btree (project_id, name) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX uq_membership_tiers_threshold ON public.membership_tiers USING btree (project_id, threshold_amount) WHERE ((deleted_at IS NULL) AND (is_default = false));

ALTER TABLE public.membership_tiers ENABLE ROW LEVEL SECURITY;

CREATE POLICY p_membership_tiers_project ON public.membership_tiers USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.membership_tiers
    ADD CONSTRAINT membership_tiers_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_membership_tiers_project_sort ON public.membership_tiers USING btree (project_id, sort_order DESC) WHERE (deleted_at IS NULL);

-- @phase comment
COMMENT ON TABLE public.membership_tiers IS '会员等级定义（BIZ-3）：按工程建键，threshold_amount 单位分（与 orders 同口径），sort_order 越大越高，每工程恰一个 is_default 等级';

COMMENT ON COLUMN public.membership_tiers.project_id IS '工程归属：等级按工程定义，同一访客在不同工程可有不同等级（消费额口径本身也按工程）';

COMMENT ON COLUMN public.membership_tiers.sort_order IS '等级高低，越大越高；解析时按它降序取第一个满足门槛的档（排序真源，不是 threshold_amount）';

COMMENT ON COLUMN public.membership_tiers.threshold_amount IS '升级门槛（分）：工程内累计消费额达到此值进入本档；默认等级恒为 0；非默认档之间不许相同（唯一索引）';

COMMENT ON COLUMN public.membership_tiers.is_default IS '每工程恰一个默认等级（部分唯一索引承载）：无归属行与无门槛满足两种情况都回退到它';

-- @phase data
SELECT pg_catalog.setval('public.membership_tiers_id_seq', 1, false);
