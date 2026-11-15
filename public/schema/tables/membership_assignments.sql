-- table: membership_assignments（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.membership_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.membership_assignments (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    user_id bigint NOT NULL,
    tier_id bigint NOT NULL,
    source character varying(16) NOT NULL,
    assigned_at timestamp with time zone DEFAULT now() NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_membership_assignments_source CHECK (((source)::text = ANY ((ARRAY['auto'::character varying, 'manual'::character varying])::text[])))
);

ALTER TABLE ONLY public.membership_assignments FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.membership_assignments_id_seq OWNED BY public.membership_assignments.id;

ALTER TABLE ONLY public.membership_assignments ALTER COLUMN id SET DEFAULT nextval('public.membership_assignments_id_seq'::regclass);

CREATE UNIQUE INDEX uq_membership_assignments_project_user ON public.membership_assignments USING btree (project_id, user_id);

ALTER TABLE public.membership_assignments ENABLE ROW LEVEL SECURITY;

CREATE POLICY p_membership_assignments_project ON public.membership_assignments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.membership_assignments
    ADD CONSTRAINT membership_assignments_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_membership_assignments_project_tier ON public.membership_assignments USING btree (project_id, tier_id);

-- @phase comment
COMMENT ON TABLE public.membership_assignments IS '会员归属（BIZ-3）：(project_id, user_id) 唯一 —— 一个访客在一个工程只能有一个当前等级';

COMMENT ON COLUMN public.membership_assignments.user_id IS '访客账号 id（users.id）；刻意不建外键（全库形态是应用层维护，且归属不该因账号注销而消失）';

COMMENT ON COLUMN public.membership_assignments.source IS 'auto=日结按消费额重算；manual=后台手工指定。手工行不被自动重算覆盖，语义由原子 SQL 的 WHERE source <> ''manual'' 承载';

COMMENT ON COLUMN public.membership_assignments.assigned_at IS '归属生效时刻（业务时刻，与 orders.paid_at 同族的 *_at 命名）；管理时间列另有 create_time / update_time';

-- @phase data
SELECT pg_catalog.setval('public.membership_assignments_id_seq', 1, false);
