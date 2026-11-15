-- table: sys_rule_assignment（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_rule_assignment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_rule_assignment (
    id bigint NOT NULL,
    rule_id bigint NOT NULL,
    target_type smallint NOT NULL,
    target_id bigint NOT NULL,
    target_scope smallint DEFAULT 0 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone
);

ALTER SEQUENCE public.sys_rule_assignment_id_seq OWNED BY public.sys_rule_assignment.id;

ALTER TABLE ONLY public.sys_rule_assignment ALTER COLUMN id SET DEFAULT nextval('public.sys_rule_assignment_id_seq'::regclass);

-- @phase constr
ALTER TABLE ONLY public.sys_rule_assignment
    ADD CONSTRAINT sys_rule_assignment_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_sys_rule_assignment_rule_id ON public.sys_rule_assignment USING btree (rule_id);

CREATE INDEX idx_sys_rule_assignment_target_id ON public.sys_rule_assignment USING btree (target_id);

CREATE INDEX idx_sys_rule_assignment_target_type ON public.sys_rule_assignment USING btree (target_type);

-- @phase comment
COMMENT ON TABLE public.sys_rule_assignment IS '数据规则分配表';

COMMENT ON COLUMN public.sys_rule_assignment.target_type IS '目标类型：1角色 2用户 3部门';

COMMENT ON COLUMN public.sys_rule_assignment.target_scope IS '作用范围：0仅本部门 1本部门及子部门';

COMMENT ON COLUMN public.sys_rule_assignment.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

-- @phase data
SELECT pg_catalog.setval('public.sys_rule_assignment_id_seq', 1, false);
