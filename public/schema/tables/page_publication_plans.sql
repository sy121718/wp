-- table: page_publication_plans（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_publication_plans (
    page_id uuid NOT NULL,
    lang text NOT NULL,
    plan jsonb NOT NULL,
    plan_hash text DEFAULT ''::text NOT NULL,
    draft_version bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.page_publication_plans
    ADD CONSTRAINT page_publication_plans_pkey PRIMARY KEY (page_id, lang);

-- @phase idx
CREATE INDEX idx_page_publication_plans_lang ON public.page_publication_plans USING btree (lang);

-- @phase comment
COMMENT ON TABLE public.page_publication_plans IS '页面每语言的发布计划冻结（审计 I18N-01）：主键 (page_id, lang)；重建入口以冻结值为准，不回读当前站点语言配置';

COMMENT ON COLUMN public.page_publication_plans.plan IS '冻结的发布计划 JSON：siteLangs（默认语言在前）+ defaultLang';

COMMENT ON COLUMN public.page_publication_plans.plan_hash IS '规范化计划的内容指纹（pipeline.PublicationPlan.Hash）';

COMMENT ON COLUMN public.page_publication_plans.draft_version IS '冻结时的草稿版本；草稿改动后下一次构建重新冻结';
