-- table: product_attributes（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_attributes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    key text NOT NULL,
    name text NOT NULL,
    is_variation boolean DEFAULT true NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    "values" jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_attributes_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_attributes_updated_at_not_null NOT NULL,
    CONSTRAINT product_attributes_is_variation_check CHECK ((is_variation = ANY (ARRAY[true, false])))
);

ALTER TABLE ONLY public.product_attributes FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_product_attributes_project_key ON public.product_attributes USING btree (project_id, key);

CREATE UNIQUE INDEX uq_product_attributes_project_key_nonempty ON public.product_attributes USING btree (project_id, key) WHERE (btrim(key) <> ''::text);

ALTER TABLE public.product_attributes ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.product_attributes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.product_attributes
    ADD CONSTRAINT product_attributes_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.product_attributes
    ADD CONSTRAINT product_attributes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

-- @phase comment
COMMENT ON TABLE public.product_attributes IS '商品属性（组 + 值合一，值走 JSON；is_variation 决定是否参与变体生成）';

COMMENT ON COLUMN public.product_attributes.key IS '属性组稳定标识（工程内唯一，小写连字符；参与变体的组会被写入变体规格组合 option_values）';

COMMENT ON COLUMN public.product_attributes.is_variation IS '是否参与变体生成（issue #7）：true=参与笛卡尔积（颜色/尺寸），false=仅展示（材质/产地）';

COMMENT ON COLUMN public.product_attributes."values" IS '属性值数组 [{id,key,label,sort,enabled}]（issue #7）；历史纯字符串数组读取时兜底归一';
