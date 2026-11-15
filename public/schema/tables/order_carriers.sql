-- table: order_carriers（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.order_carriers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    tracking_url_template text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_order_carriers_code CHECK ((btrim(code) <> ''::text)),
    CONSTRAINT ck_order_carriers_status CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text])))
);

ALTER TABLE ONLY public.order_carriers FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_order_carriers_project_code ON public.order_carriers USING btree (project_id, upper(code));

ALTER TABLE public.order_carriers ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.order_carriers USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.order_carriers
    ADD CONSTRAINT order_carriers_pkey PRIMARY KEY (id);
