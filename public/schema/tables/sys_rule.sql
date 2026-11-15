-- table: sys_rule（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_rule_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_rule (
    id bigint NOT NULL,
    rule_name character varying(100) NOT NULL,
    domain character varying(50) NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);

ALTER SEQUENCE public.sys_rule_id_seq OWNED BY public.sys_rule.id;

ALTER TABLE ONLY public.sys_rule ALTER COLUMN id SET DEFAULT nextval('public.sys_rule_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_rule
    ADD CONSTRAINT sys_rule_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_sys_rule_domain ON public.sys_rule USING btree (domain);

CREATE INDEX idx_sys_rule_status ON public.sys_rule USING btree (status);

-- @phase comment
COMMENT ON TABLE public.sys_rule IS '数据规则表';

COMMENT ON COLUMN public.sys_rule.rule_name IS '规则名称';

COMMENT ON COLUMN public.sys_rule.domain IS '规则领域';

COMMENT ON COLUMN public.sys_rule.config IS '规则配置（JSON）';

COMMENT ON COLUMN public.sys_rule.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_rule.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.sys_rule_id_seq', 1, false);
