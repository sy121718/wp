-- table: master_data_changes（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.master_data_changes (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
)
PARTITION BY RANGE (create_time);

ALTER TABLE ONLY public.master_data_changes FORCE ROW LEVEL SECURITY;

CREATE TRIGGER trg_master_data_changes_append_only BEFORE DELETE OR UPDATE ON public.master_data_changes FOR EACH ROW EXECUTE FUNCTION public.master_data_changes_append_only();

ALTER TABLE public.master_data_changes ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.master_data_changes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.master_data_changes
    ADD CONSTRAINT master_data_changes_pkey1 PRIMARY KEY (id, create_time);

-- @phase idx
CREATE INDEX idx_master_data_changes_entity ON ONLY public.master_data_changes USING btree (project_id, entity_type, entity_id, create_time DESC);

CREATE INDEX idx_master_data_changes_field ON ONLY public.master_data_changes USING btree (project_id, entity_type, field, create_time DESC);

CREATE INDEX idx_master_data_changes_label_trgm ON ONLY public.master_data_changes USING gin (entity_label ext_shared.gin_trgm_ops);

CREATE INDEX idx_master_data_changes_time ON ONLY public.master_data_changes USING btree (project_id, create_time DESC);

-- @phase comment
COMMENT ON TABLE public.master_data_changes IS '主数据变更记录（issue #19；按月分区 + append-only 触发器）：字段级审计';
