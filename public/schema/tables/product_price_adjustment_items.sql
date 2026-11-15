-- table: product_price_adjustment_items（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_price_adjustment_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    adjustment_id uuid NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    old_price numeric(12,2) NOT NULL,
    new_price numeric(12,2) NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_price_adjustment_items_created_at_not_null NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT product_price_adjustment_items_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_product_price_adjustment_items_adjustment ON public.product_price_adjustment_items USING btree (adjustment_id);

CREATE INDEX idx_product_price_adjustment_items_variant ON public.product_price_adjustment_items USING btree (variant_id, create_time DESC);

-- @phase fk
ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT fk_product_price_adjustment_items_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT fk_product_price_adjustment_items_variant FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT product_price_adjustment_items_adjustment_id_fkey FOREIGN KEY (adjustment_id) REFERENCES public.product_price_adjustments(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.product_price_adjustment_items IS '调价批次逐变体明细（原价 → 新价）';
