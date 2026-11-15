-- table: inventory_transfers（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.inventory_transfers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    transfer_no text NOT NULL,
    from_warehouse_id uuid NOT NULL,
    to_warehouse_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    shipped_by bigint DEFAULT 0 NOT NULL,
    received_by bigint DEFAULT 0 NOT NULL,
    shipped_at timestamp with time zone,
    received_at timestamp with time zone,
    request_id text DEFAULT ''::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    requested_by bigint DEFAULT 0 NOT NULL,
    requested_at timestamp with time zone,
    approved_by bigint DEFAULT 0 NOT NULL,
    approved_at timestamp with time zone,
    approved_name text DEFAULT ''::text NOT NULL,
    rejected_by bigint DEFAULT 0 NOT NULL,
    rejected_at timestamp with time zone,
    rejected_name text DEFAULT ''::text NOT NULL,
    rejected_reason text DEFAULT ''::text NOT NULL,
    CONSTRAINT ck_inventory_transfers_diff_wh CHECK ((from_warehouse_id <> to_warehouse_id)),
    CONSTRAINT ck_inventory_transfers_status CHECK ((status = ANY (ARRAY['requested'::text, 'pending'::text, 'in_transit'::text, 'completed'::text, 'rejected'::text, 'cancelled'::text])))
);

ALTER TABLE ONLY public.inventory_transfers FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_inventory_transfers_no ON public.inventory_transfers USING btree (project_id, transfer_no);

CREATE UNIQUE INDEX uq_inventory_transfers_request ON public.inventory_transfers USING btree (project_id, request_id) WHERE (request_id <> ''::text);

ALTER TABLE public.inventory_transfers ENABLE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON public.inventory_transfers USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.inventory_transfers
    ADD CONSTRAINT inventory_transfers_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_inventory_transfers_requested ON public.inventory_transfers USING btree (project_id, create_time DESC) WHERE (status = 'requested'::text);

CREATE INDEX idx_inventory_transfers_status ON public.inventory_transfers USING btree (project_id, status, create_time DESC);

-- @phase comment
COMMENT ON TABLE public.inventory_transfers IS '调拨单（仓库间移库）：pending → in_transit → completed，或 pending → cancelled';

COMMENT ON COLUMN public.inventory_transfers.status IS '调拨单状态：requested 待审批 / pending 待出货 / in_transit 在途 / completed 已完成 / rejected 已驳回 / cancelled 已取消';

COMMENT ON COLUMN public.inventory_transfers.request_id IS '幂等键：空串 = 没有幂等键；非空时同工程唯一';

COMMENT ON COLUMN public.inventory_transfers.requested_by IS '申请人（建单人）：0 = 630 时代的存量单据未记';

COMMENT ON COLUMN public.inventory_transfers.approved_by IS '审批人：0 表示尚未审批（或被驳回）';

COMMENT ON COLUMN public.inventory_transfers.rejected_reason IS '驳回原因（纯文本，运营填给申请人看）；仅 rejected 态有值';
