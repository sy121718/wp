-- table: product_variants（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.product_variants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    product_id uuid NOT NULL,
    sku_code text NOT NULL,
    barcode text DEFAULT ''::text NOT NULL,
    price numeric(12,2) DEFAULT 0 NOT NULL,
    compare_price numeric(12,2),
    cost_price numeric(12,2),
    image text DEFAULT ''::text NOT NULL,
    option_values jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_variants_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_variants_updated_at_not_null NOT NULL,
    warehouse_skus jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT product_variants_warehouse_skus_shape_check CHECK (((jsonb_typeof(warehouse_skus) = 'array'::text) AND (NOT jsonb_path_exists(warehouse_skus, '$[*]?(((!(exists ($."stockId")) || !(exists ($."warehouseId"))) || $."stockId" == "") || $."warehouseId" == "")'::jsonpath))))
);

CREATE UNIQUE INDEX uq_product_variants_product_options ON public.product_variants USING btree (product_id, option_values) WHERE (option_values <> '{}'::jsonb);

-- @phase constr
ALTER TABLE ONLY public.product_variants
    ADD CONSTRAINT product_variants_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.product_variants
    ADD CONSTRAINT product_variants_product_id_sku_code_key UNIQUE (product_id, sku_code);

-- @phase idx
CREATE INDEX idx_product_variants_option_values ON public.product_variants USING gin (option_values jsonb_path_ops);

CREATE INDEX idx_product_variants_price_enabled ON public.product_variants USING btree (product_id, price) WHERE enabled;

CREATE INDEX idx_product_variants_product ON public.product_variants USING btree (product_id);

CREATE INDEX idx_product_variants_warehouse_skus ON public.product_variants USING gin (warehouse_skus jsonb_path_ops);

-- @phase fk
ALTER TABLE ONLY public.product_variants
    ADD CONSTRAINT product_variants_product_id_fkey FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.product_variants IS '商品变体 / SKU（一行一个；可用量只读 inventory_stocks 真源，121 起无 stock_total 列）';

COMMENT ON COLUMN public.product_variants.warehouse_skus IS '本变体引用的仓库货行（JSON 数组，元素 {stockId,warehouseId,warehouseCode,skuCode}）；多对多，取代 624 的认领；反查走 idx_product_variants_warehouse_skus';

COMMENT ON INDEX public.idx_product_variants_option_values IS '属性值筛选（issue #25）：EXISTS 子查询的 @> 包含判断';

COMMENT ON INDEX public.idx_product_variants_price_enabled IS '价格区间筛选与价格排序（issue #28）：只覆盖启用变体';
