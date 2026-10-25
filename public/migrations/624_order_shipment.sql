-- 624 · 履约事实：承运商 + 发货单 + 发货明细（对齐 ERP ord_shipment，不含 fulfill_status）。
--
-- 为什么需要独立的发货单而不是只改 orders.status：
--   · 一张订单可以多次发货（部分发货 / 分仓），状态列只能表达「最后一次」；
--   · 库存流水的 source_ref 需要一个可回指的凭据号（否则「这批货为什么少了 3 件」
--     只能靠时间猜）；
--   · 承运商与运单号是客户可见事实，必须与操作人、时间一起留痕。
--
-- 刻意不加的列：fulfill_status（本地只有一条状态链，order.go 的 allowedTransitions）；
--   reserved 相关（预占在 inventory_stocks，见迁移 621）。
--
-- 幂等：全部 IF NOT EXISTS；不加外键到 orders（与 order_items 同口径：订单不因局部清理而消失）。

CREATE TABLE IF NOT EXISTS order_carriers (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id            uuid NOT NULL,
    code                  text NOT NULL,
    name                  text NOT NULL,
    -- 跟踪页模板，唯一允许的占位符是 {tracking_no}；空串表示「没有公开跟踪页」。
    tracking_url_template text NOT NULL DEFAULT '',
    status                text NOT NULL DEFAULT 'active',
    sort                  integer NOT NULL DEFAULT 0,
    metadata              jsonb NOT NULL DEFAULT '{}'::jsonb,
    create_time           timestamptz NOT NULL DEFAULT now(),
    update_time           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_order_carriers_status CHECK (status IN ('active', 'disabled')),
    CONSTRAINT ck_order_carriers_code   CHECK (btrim(code) <> '')
);

-- 工程内承运商代码唯一（按 upper 归一）：能建两个「SF」只会让运营在发货表单里选错。
CREATE UNIQUE INDEX IF NOT EXISTS uq_order_carriers_project_code
    ON order_carriers (project_id, upper(code));

CREATE TABLE IF NOT EXISTS order_shipments (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    uuid NOT NULL,
    order_id      bigint NOT NULL,
    shipment_no   text NOT NULL,
    -- 承运商可能被停用 / 删除：单据保留当时的名称快照，展示不依赖关联行。
    carrier_id    uuid,
    carrier_name  text NOT NULL DEFAULT '',
    tracking_no   text NOT NULL DEFAULT '',
    shipped_at    timestamptz NOT NULL DEFAULT now(),
    operator_id   bigint NOT NULL DEFAULT 0,
    operator_name text NOT NULL DEFAULT '',
    remark        text NOT NULL DEFAULT '',
    -- 幂等键：前端不传时留空串；非空时同一 (project_id, request_id) 只允许一张单。
    request_id    text NOT NULL DEFAULT '',
    create_time   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_order_shipments_no
    ON order_shipments (project_id, shipment_no);
-- 部分唯一索引而不是普通唯一索引：空串是「没有幂等键」这个合法状态，
-- 普通唯一索引会让同一工程只能有一张「不带 request_id」的发货单。
CREATE UNIQUE INDEX IF NOT EXISTS uq_order_shipments_request
    ON order_shipments (project_id, request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS idx_order_shipments_order
    ON order_shipments (order_id, create_time DESC);

CREATE TABLE IF NOT EXISTS order_shipment_items (
    id            bigserial PRIMARY KEY,
    shipment_id   uuid NOT NULL REFERENCES order_shipments(id) ON DELETE CASCADE,
    order_item_id bigint NOT NULL,
    -- 仓库是发货事实的一部分（order_items 也有 warehouse_id，见 625）。
    warehouse_id  uuid NOT NULL,
    -- 仓库商品 SKU 行快照（对齐 ERP 170_order_shipment_item_warehouse_sku）。
    -- 不建外键：库存行可能随变体删除而级联消失，但发货事实必须留得住。
    stock_id      uuid,
    quantity      integer NOT NULL,
    CONSTRAINT ck_order_shipment_items_qty CHECK (quantity > 0)
);

-- 同一张发货单里，同一条订单行的同一个仓只允许出现一次：那是同一份事实的重复记账。
CREATE UNIQUE INDEX IF NOT EXISTS uq_order_shipment_items_row
    ON order_shipment_items (shipment_id, order_item_id, warehouse_id);
CREATE INDEX IF NOT EXISTS idx_order_shipment_items_item
    ON order_shipment_items (order_item_id);

-- 订单头只补「最近一次发货」的快照列：历史发货看 order_shipments，不看订单头。
ALTER TABLE orders ADD COLUMN IF NOT EXISTS carrier_id   uuid;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS carrier_name text NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS tracking_no  text NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS shipped_at   timestamptz;

COMMENT ON COLUMN orders.carrier_name IS '最近一次发货的承运商名称快照（历史发货看 order_shipments）';
COMMENT ON COLUMN orders.tracking_no IS '最近一次发货的运单号快照（历史发货看 order_shipments）';
COMMENT ON COLUMN orders.shipped_at IS '最近一次发货时刻（历史发货看 order_shipments）';

-- RLS：两张新表都是工程维度（与 215 同口径，fail closed）。
--
-- 为什么要在这里显式写（而不是等 215 的名单）：215 用 information_schema 枚举当时已存在的表，
-- 后来新建的表不在它的快照里。两张表存的是客户与承运商事实，缺策略 = 任何工程都能读别人的。
ALTER TABLE order_carriers ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_carriers FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS project_isolation ON order_carriers;
CREATE POLICY project_isolation ON order_carriers
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

ALTER TABLE order_shipments ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_shipments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS project_isolation ON order_shipments;
CREATE POLICY project_isolation ON order_shipments
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

-- order_shipment_items 不带 project_id（与 order_items 同形）：RLS 靠 join order_shipments 判定。
-- 给它加 project_id 会让子表形状与 order_items 分叉，且没有第二处真源可对账。
