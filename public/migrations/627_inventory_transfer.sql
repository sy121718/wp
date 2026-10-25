-- 627 · 调拨单（仓库间移库）+ 明细 + 凭证附件（W3）。
--
-- 为什么明细指向库存行（stock_id）而不是变体（variant_id）：
--   对齐 ERP 迁移 168（prd_transfer_item.sku_id → warehouse_sku_id，「仓库商品 SKU ID，源仓」）。
--   调拨搬的是**源仓里那一批具体的货**，不是「某个变体」——指向库存行才能表达
--   「这批未认领的货也能调拨」，也才能把「源仓这一行」与「目标仓那一行」对上。
--   variant_id / sku_code 作为快照冗余：库存行被删（如变体删除级联）后单据仍能回显出搬的是什么。
--
-- 为什么凭证单独一张表而不是塞 metadata：
--   一张单据可以有多种用途的凭证（申请 / 出货 / 签收），还要多重、要排序、要校验存在性；
--   metadata 里的 JSON 无法参与存在性校验，也无法被附件页统一管理。
--
-- 建单即定两仓（与 ERP 的一处刻意差异）：ERP 的 Create 只校验源仓、目标仓到 Receive 才给；
--   本地是单仓管理员操作，建单时不知道发往哪里等于留一张半截单据，而且目标仓是审批与凭证
--   都需要的上下文。from <> to 由 DDL CHECK 钉住，应用层不必记着判。
--
-- 幂等：全部 IF NOT EXISTS；RLS 用 DROP POLICY IF EXISTS + CREATE。

CREATE TABLE IF NOT EXISTS inventory_transfers (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL,
    transfer_no       text NOT NULL,
    from_warehouse_id uuid NOT NULL,
    to_warehouse_id   uuid NOT NULL,
    status            text NOT NULL DEFAULT 'pending',
    remark            text NOT NULL DEFAULT '',
    create_by         bigint NOT NULL DEFAULT 0,
    shipped_by        bigint NOT NULL DEFAULT 0,
    received_by       bigint NOT NULL DEFAULT 0,
    shipped_at        timestamptz,
    received_at       timestamptz,
    request_id        text NOT NULL DEFAULT '',
    create_time       timestamptz NOT NULL DEFAULT now(),
    update_time       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_inventory_transfers_status
        CHECK (status IN ('pending', 'in_transit', 'completed', 'cancelled')),
    -- 源仓与目标仓相同 = 自己搬给自己：DDL 层直接拒绝，不靠应用层记着判。
    CONSTRAINT ck_inventory_transfers_diff_wh CHECK (from_warehouse_id <> to_warehouse_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_transfers_no
    ON inventory_transfers (project_id, transfer_no);
-- 部分唯一索引：空串是「没有幂等键」这个合法状态（同 624 的 order_shipments）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_transfers_request
    ON inventory_transfers (project_id, request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS idx_inventory_transfers_status
    ON inventory_transfers (project_id, status, create_time DESC);

CREATE TABLE IF NOT EXISTS inventory_transfer_items (
    id            bigserial PRIMARY KEY,
    transfer_id   uuid NOT NULL REFERENCES inventory_transfers(id) ON DELETE CASCADE,
    -- 源仓的库存行 id（仓库商品 SKU）。不建外键：库存行可随变体删除级联消失，
    -- 但调拨单据是已发生的事实，必须留得住。
    stock_id      uuid NOT NULL,
    -- 变体快照：未认领行（variant_id IS NULL，迁移 621）也允许调拨，所以这里可空。
    variant_id    uuid,
    sku_code      text NOT NULL DEFAULT '',
    quantity      integer NOT NULL,
    CONSTRAINT ck_inventory_transfer_items_qty CHECK (quantity > 0)
);

-- 同一张调拨单里，同一条源仓库存行只允许出现一次：那是同一份事实的重复记账。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_transfer_items_row
    ON inventory_transfer_items (transfer_id, stock_id);

CREATE TABLE IF NOT EXISTS inventory_transfer_attachments (
    id            bigserial PRIMARY KEY,
    transfer_id   uuid NOT NULL REFERENCES inventory_transfers(id) ON DELETE CASCADE,
    -- sys_attachment.id（不建外键：附件表属 media 模块，跨模块不建约束 —— AGENTS.md 的表归属判据）。
    attachment_id bigint NOT NULL,
    -- request 申请 / ship 出货 / receive 签收（对齐 ERP 131 的 usage_type，另加 request）。
    usage_type    text NOT NULL,
    sort          integer NOT NULL DEFAULT 0,
    create_time   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_inventory_transfer_attachments_usage
        CHECK (usage_type IN ('request', 'ship', 'receive'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_transfer_attachments_row
    ON inventory_transfer_attachments (transfer_id, attachment_id, usage_type);

-- RLS（与 215 同口径，fail closed）。两张子表不带 project_id，靠父表 join 判定 ——
-- 给它们加 project_id 会让形状与 order_items / order_shipment_items 分叉，
-- 且没有第二处真源可对账。
ALTER TABLE inventory_transfers ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory_transfers FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS project_isolation ON inventory_transfers;
CREATE POLICY project_isolation ON inventory_transfers
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

COMMENT ON TABLE inventory_transfers IS '调拨单（仓库间移库）：pending → in_transit → completed，或 pending → cancelled';
COMMENT ON COLUMN inventory_transfers.request_id IS '幂等键：空串 = 没有幂等键；非空时同工程唯一';
COMMENT ON TABLE inventory_transfer_attachments IS '调拨单凭证：出货与签收各需至少一张（service 强制），申请凭证可选';
