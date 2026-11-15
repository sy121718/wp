-- table: inventory_stocks（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_stocks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    product_id uuid,
    variant_id uuid,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stocks_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stocks_updated_at_not_null NOT NULL,
    cost_price numeric(12,2),
    external_sku text DEFAULT ''::text NOT NULL,
    track_quantity boolean DEFAULT false NOT NULL,
    reserved_quantity integer DEFAULT 0 NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    image_url text DEFAULT ''::text NOT NULL,
    barcode text DEFAULT ''::text NOT NULL,
    freight_price numeric(12,2) DEFAULT 70 NOT NULL,
    cost_currency text DEFAULT 'CNY'::text NOT NULL,
    cost_base_currency text DEFAULT 'CNY'::text NOT NULL,
    cost_fx_rate numeric(18,8),
    status text DEFAULT 'active'::text NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    CONSTRAINT ck_inventory_stocks_cost_currency CHECK (((btrim(cost_currency) <> ''::text) AND (btrim(cost_base_currency) <> ''::text))),
    CONSTRAINT ck_inventory_stocks_cost_fx_rate CHECK (((cost_fx_rate IS NULL) OR (cost_fx_rate > (0)::numeric))),
    CONSTRAINT ck_inventory_stocks_freight_nonneg CHECK ((freight_price >= (0)::numeric)),
    CONSTRAINT ck_inventory_stocks_reserved_le_quantity CHECK ((reserved_quantity <= quantity)),
    CONSTRAINT ck_inventory_stocks_reserved_nonneg CHECK ((reserved_quantity >= 0)),
    CONSTRAINT inventory_stocks_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text]))),
    CONSTRAINT inventory_stocks_track_quantity_check CHECK ((track_quantity OR (quantity = 0)))
);

ALTER TABLE ONLY public.inventory_stocks FORCE ROW LEVEL SECURITY;

ALTER TABLE public.inventory_stocks ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_stocks USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT uq_inventory_stocks_warehouse_sku UNIQUE (warehouse_id, sku_code);

-- @phase idx
CREATE INDEX idx_inventory_stocks_name ON public.inventory_stocks USING btree (project_id, warehouse_id, name);

CREATE INDEX idx_inventory_stocks_product ON public.inventory_stocks USING btree (product_id);

CREATE INDEX idx_inventory_stocks_project_sku ON public.inventory_stocks USING btree (project_id, sku_code);

CREATE INDEX idx_inventory_stocks_warehouse ON public.inventory_stocks USING btree (warehouse_id);

CREATE INDEX idx_inventory_stocks_warehouse_external_sku ON public.inventory_stocks USING btree (warehouse_id, external_sku);

CREATE INDEX idx_inventory_stocks_warehouse_nonzero ON public.inventory_stocks USING btree (warehouse_id) WHERE (quantity <> 0);

-- @phase fk
ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT fk_inventory_stocks_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_variant_id_fkey FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_warehouse_id_fkey FOREIGN KEY (warehouse_id) REFERENCES public.inventory_warehouses(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.inventory_stocks IS '库存真源（SKU × 仓库）；商品侧不再有缓存副本，展示值按需投影（issue #32）';

COMMENT ON COLUMN public.inventory_stocks.product_id IS '这条货归属哪个商品（线索列，641 起不再唯一、不参与定位）';

COMMENT ON COLUMN public.inventory_stocks.variant_id IS '这条货卖出 / 归属于哪个变体（线索列，641 起不再唯一、不参与定位）；真正的多对多关联在 product_variants.warehouse_skus';

COMMENT ON COLUMN public.inventory_stocks.quantity IS '可用量真源；一切可用量判断只读本列并加行锁，绝不读 product_variants.stock_total';

COMMENT ON COLUMN public.inventory_stocks.cost_price IS '当前成本价（(仓库, SKU) 一个值，覆盖式，最近一次入库 / 显式写入为准）；NULL = 尚未核算，0 是合法的显式成本';

COMMENT ON COLUMN public.inventory_stocks.external_sku IS '这条库存在该仓的外部 / 第三方编码（N:1：同一商品的多个变体可共用；空串 = 该仓用我们自己的 SKU）';

COMMENT ON COLUMN public.inventory_stocks.track_quantity IS '是否跟踪数量：false = 无限（不跟踪，扣减不校验可用量也不扣减，quantity 恒为 0）；true = 按 quantity 跟踪。存量行一律 true（保守，见迁移 261 注释），新建行默认 false。';

COMMENT ON COLUMN public.inventory_stocks.reserved_quantity IS '本仓预占数量（被未发货订单占住）；可售 = quantity - reserved_quantity；预占/释放只改本列，不改 quantity、不写流水';

COMMENT ON COLUMN public.inventory_stocks.name IS '仓库侧的商品名（642，对齐 dy_erp prd_warehouse_sku.name）：这条货在这个仓叫什么；可以先于商品存在';

COMMENT ON COLUMN public.inventory_stocks.image_url IS '仓库侧的货品图（642）：同一款货在不同仓可以有不同的图（对方仓要求的图）';

COMMENT ON COLUMN public.inventory_stocks.barcode IS '条码（642）：扫码枪读到的原始串，不参与任何唯一约束（同一条码可以贴多行）';

COMMENT ON COLUMN public.inventory_stocks.freight_price IS '运费（642）：这条货从仓到买家的固定开销，默认 70（照 dy_erp）；不计入 cost_price';

COMMENT ON COLUMN public.inventory_stocks.cost_currency IS '成本的计价币种（642）：记录 cost_price 用什么币记的';

COMMENT ON COLUMN public.inventory_stocks.cost_base_currency IS '换算的基准币种（642）：与 cost_fx_rate 一起表达「1 计价币 = fx 基准币」';

COMMENT ON COLUMN public.inventory_stocks.cost_fx_rate IS '成本换算汇率（642）：NULL = 汇率未固化，不推算（与 orders.fx_rate 同口径）';

COMMENT ON COLUMN public.inventory_stocks.status IS '货行状态（642）：disabled 不再出现在「从仓库选」候选里，但历史流水与已建立的变体引用照旧可读';

COMMENT ON COLUMN public.inventory_stocks.remark IS '备注（642）';

COMMENT ON INDEX public.idx_inventory_stocks_warehouse_nonzero IS '删仓守卫：非零库存计数（审计 IDX-008）';
