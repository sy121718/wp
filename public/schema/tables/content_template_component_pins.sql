-- table: content_template_component_pins（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.content_template_component_pins (
    template_id uuid NOT NULL,
    component_id uuid NOT NULL,
    pinned_version_id uuid NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.content_template_component_pins
    ADD CONSTRAINT content_template_component_pins_pkey PRIMARY KEY (template_id, component_id);

-- @phase fk
ALTER TABLE ONLY public.content_template_component_pins
    ADD CONSTRAINT content_template_component_pins_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.content_templates(id);
