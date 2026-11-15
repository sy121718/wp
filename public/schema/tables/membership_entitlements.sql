-- table: membership_entitlements（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.membership_entitlements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.membership_entitlements (
    id bigint NOT NULL,
    tier_id bigint NOT NULL,
    kind character varying(32) NOT NULL,
    value_int bigint NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_membership_entitlements_value CHECK (((((kind)::text = 'free_shipping'::text) AND (value_int = ANY (ARRAY[(0)::bigint, (1)::bigint]))) OR (((kind)::text = 'discount'::text) AND ((value_int >= 1) AND (value_int <= 100)))))
);

ALTER SEQUENCE public.membership_entitlements_id_seq OWNED BY public.membership_entitlements.id;

ALTER TABLE ONLY public.membership_entitlements ALTER COLUMN id SET DEFAULT nextval('public.membership_entitlements_id_seq'::regclass);

CREATE UNIQUE INDEX uq_membership_entitlements_tier_kind ON public.membership_entitlements USING btree (tier_id, kind);

-- @phase constr
ALTER TABLE ONLY public.membership_entitlements
    ADD CONSTRAINT membership_entitlements_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON TABLE public.membership_entitlements IS '等级权益（BIZ-3）：一期两种 kind（free_shipping / discount），整数列 + CHECK 限值而不是 JSONB（本表参与金额计算）';

COMMENT ON COLUMN public.membership_entitlements.tier_id IS '所属等级（membership_tiers.id）；刻意不建外键，与 page_schedules 等台账同口径（硬失败会卡住已删等级的关联行回收）';

COMMENT ON COLUMN public.membership_entitlements.kind IS '权益类型：free_shipping=免运费；discount=折扣百分比。唯一键 (tier_id, kind)：改权益是 upsert，不是追加';

COMMENT ON COLUMN public.membership_entitlements.value_int IS '按 kind 解释：free_shipping 取 0/1；discount 取 1..100（20 = 打八折，即扣减 20%）。越界由 ck_membership_entitlements_value 拒绝';
