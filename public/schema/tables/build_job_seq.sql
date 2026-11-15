-- table: build_job_seq（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.build_job_seq (
    project_id uuid NOT NULL,
    kind character varying(32) NOT NULL,
    next_version bigint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_build_job_seq_kind CHECK (((kind)::text = ANY ((ARRAY['header'::character varying, 'header_mobile'::character varying, 'footer'::character varying, 'footer_mobile'::character varying])::text[])))
);

ALTER TABLE ONLY public.build_job_seq FORCE ROW LEVEL SECURITY;

ALTER TABLE public.build_job_seq ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.build_job_seq USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.build_job_seq
    ADD CONSTRAINT build_job_seq_pkey PRIMARY KEY (project_id, kind);

-- @phase fk
ALTER TABLE ONLY public.build_job_seq
    ADD CONSTRAINT build_job_seq_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.build_job_seq IS 'menu_stale 的输入版本号源：每 (工程, 菜单位置) 一行，事务内原子 UPSERT 递增取号';

COMMENT ON COLUMN public.build_job_seq.next_version IS '上一次分配出去的输入版本号；每次真实变化 +1，与任务行同事务提交（回滚即未消耗）';
