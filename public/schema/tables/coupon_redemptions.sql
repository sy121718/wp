-- table: coupon_redemptions（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.coupon_redemptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.coupon_redemptions (
    id bigint NOT NULL,
    coupon_id bigint NOT NULL,
    project_id uuid NOT NULL,
    code character varying(64) NOT NULL,
    order_id bigint NOT NULL,
    order_no character varying(40) DEFAULT ''::character varying NOT NULL,
    discount_amount bigint DEFAULT 0 NOT NULL,
    user_id bigint,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_coupon_redemptions_amount CHECK ((discount_amount >= 0))
);

ALTER TABLE ONLY public.coupon_redemptions FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.coupon_redemptions_id_seq OWNED BY public.coupon_redemptions.id;

ALTER TABLE ONLY public.coupon_redemptions ALTER COLUMN id SET DEFAULT nextval('public.coupon_redemptions_id_seq'::regclass);

CREATE UNIQUE INDEX uq_coupon_redemptions_coupon_order ON public.coupon_redemptions USING btree (coupon_id, order_id);

ALTER TABLE public.coupon_redemptions ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.coupon_redemptions USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.coupon_redemptions
    ADD CONSTRAINT coupon_redemptions_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_coupon_redemptions_project ON public.coupon_redemptions USING btree (project_id, coupon_id);

CREATE INDEX idx_coupon_redemptions_user ON public.coupon_redemptions USING btree (coupon_id, user_id);

-- @phase comment
COMMENT ON COLUMN public.coupon_redemptions.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';
