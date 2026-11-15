-- table: page_component_pins（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.page_component_pins (
    page_id uuid NOT NULL,
    component_id uuid NOT NULL,
    pinned_version_id uuid NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.page_component_pins
    ADD CONSTRAINT page_component_pins_pkey PRIMARY KEY (page_id, component_id);

-- @phase fk
ALTER TABLE ONLY public.page_component_pins
    ADD CONSTRAINT page_component_pins_page_id_fkey FOREIGN KEY (page_id) REFERENCES public.pages(id);
