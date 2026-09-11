-- 108 · 采购单与入库（issue #18）。
--
-- 四张表承接「下采购单 → 按行登记入库 → 自动增加库存并更新 SKU 成本价」这条链路：
--
--   1. inventory_purchase_orders      采购单头（单号 / 货源 / 收货仓 / 状态 / 下单人）
--   2. inventory_purchase_order_lines 采购行（SKU × 采购数量 × 采购单价 × **已入库数量**）
--   3. inventory_purchase_receipts    入库单头（采购收货 / 自家工厂生产入库，含幂等键）
--   4. inventory_purchase_receipt_items 入库单行（数量 / 单价快照 / 成本价回写结果）
--
-- 三条不可动摇的语义：
--
--   · 状态是**推导值**：采购单状态由「已入库数量 与 采购数量」推出（pending / partial /
--     received），不是人工改的状态机。DDL 用 received_quantity BETWEEN 0 AND quantity
--     兜住「已入库不得超过采购数量」，服务层在同一事务内重算状态并写回。
--   · 入库是**幂等**的：receipts.request_id 是重放保护键（工程内唯一，空串不参与唯一性），
--     同一个 key 重复提交命中既有入库单并原样返回，不会第二次动库存。
--   · 入库单必带**来源**：source_id 指向 #17 的货源（外部供应商 / 集团内关联公司 / 自家工厂），
--     生产入库（kind = 'production'，无采购单）的成本价手工填写，采购收货的成本价取采购单价。
--
-- 库存真源仍只有 inventory_stocks 一张：入库经服务层的变动契约（ChangeStock）写它，
-- 本迁移不新增任何库存列，也绝不复制 product_variants.stock_total 那类缓存。

CREATE TABLE IF NOT EXISTS inventory_purchase_orders (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects(id),
    code         text NOT NULL,
    source_id    uuid NOT NULL REFERENCES inventory_sources(id),
    warehouse_id uuid NOT NULL REFERENCES inventory_warehouses(id),
    status       text NOT NULL DEFAULT 'pending',
    ordered_at   timestamptz NOT NULL DEFAULT now(),
    expected_at  timestamptz NULL,
    remark       text NOT NULL DEFAULT '',
    operator_id  text NOT NULL DEFAULT '',
    metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    -- 状态由「已入库数量 与 采购数量」推导，取值只有这三种（没有人工置位）。
    CONSTRAINT inventory_purchase_orders_status_check CHECK (status IN ('pending', 'partial', 'received'))
);

-- 单号工程内唯一（按 upper 归一，服务层归一成大写，索引兜住绕过服务层的写入）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_purchase_orders_project_code
    ON inventory_purchase_orders(project_id, upper(code));
-- 后台列表默认「按工程 + 状态」翻看（待入库的单最需要被看见）。
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_orders_project_status
    ON inventory_purchase_orders(project_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_orders_source
    ON inventory_purchase_orders(source_id);

CREATE TABLE IF NOT EXISTS inventory_purchase_order_lines (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          uuid NOT NULL REFERENCES inventory_purchase_orders(id) ON DELETE CASCADE,
    project_id        uuid NOT NULL REFERENCES projects(id),
    product_id        uuid NOT NULL,
    variant_id        uuid NOT NULL,
    sku_code          text NOT NULL DEFAULT '',
    quantity          integer NOT NULL,
    received_quantity integer NOT NULL DEFAULT 0,
    unit_price        numeric(12,2) NOT NULL,
    sort              integer NOT NULL DEFAULT 0,
    remark            text NOT NULL DEFAULT '',
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_purchase_order_lines_qty_check CHECK (quantity > 0),
    -- 已入库数量是**原子递增**的目标，且恒不超过采购数量（超收在数据层即不可能）。
    CONSTRAINT inventory_purchase_order_lines_received_check
        CHECK (received_quantity >= 0 AND received_quantity <= quantity),
    CONSTRAINT inventory_purchase_order_lines_price_check CHECK (unit_price >= 0)
);

-- 同一采购单里同一个 SKU 只出现一行：已入库数量的归属因此不含糊。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_purchase_lines_order_variant
    ON inventory_purchase_order_lines(order_id, variant_id);
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_lines_order
    ON inventory_purchase_order_lines(order_id, sort ASC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_lines_variant
    ON inventory_purchase_order_lines(project_id, variant_id);

CREATE TABLE IF NOT EXISTS inventory_purchase_receipts (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects(id),
    code              text NOT NULL,
    kind              text NOT NULL DEFAULT 'purchase',
    order_id          uuid NULL REFERENCES inventory_purchase_orders(id),
    source_id         uuid NOT NULL REFERENCES inventory_sources(id),
    warehouse_id      uuid NOT NULL REFERENCES inventory_warehouses(id),
    request_id        text NOT NULL DEFAULT '',
    status            text NOT NULL DEFAULT 'posted',
    movement_batch_id text NOT NULL DEFAULT '',
    remark            text NOT NULL DEFAULT '',
    operator_id       text NOT NULL DEFAULT '',
    received_at       timestamptz NOT NULL DEFAULT now(),
    metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_purchase_receipts_kind_check CHECK (kind IN ('purchase', 'production')),
    -- pending 是「记账已落库、库存变动尚未完成」的中间态：进程在两步之间中断时，
    -- 单据会停在 pending 被后台看见（绝不会有「已完成的单据没动过库存」这种假象）。
    -- 变动失败不留 failed 单据：补偿路径把单据整体删除，状态列只有这两态。
    CONSTRAINT inventory_purchase_receipts_status_check CHECK (status IN ('pending', 'posted')),
    -- 采购收货必有采购单，生产入库（自家工厂）必无：两种入库不共用单据身份。
    CONSTRAINT inventory_purchase_receipts_order_kind_check
        CHECK ((kind = 'purchase') = (order_id IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_purchase_receipts_project_code
    ON inventory_purchase_receipts(project_id, upper(code));
-- 幂等键：同一工程内同一个 request_id 至多一张入库单（空串不参与 —— 未给键时才允许重复提交）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_inventory_purchase_receipts_project_request
    ON inventory_purchase_receipts(project_id, request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_receipts_order
    ON inventory_purchase_receipts(order_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_receipts_source
    ON inventory_purchase_receipts(source_id, received_at DESC);

CREATE TABLE IF NOT EXISTS inventory_purchase_receipt_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id   uuid NOT NULL REFERENCES inventory_purchase_receipts(id) ON DELETE CASCADE,
    project_id   uuid NOT NULL REFERENCES projects(id),
    line_id      uuid NULL REFERENCES inventory_purchase_order_lines(id),
    product_id   uuid NOT NULL,
    variant_id   uuid NOT NULL,
    sku_code     text NOT NULL DEFAULT '',
    quantity     integer NOT NULL,
    unit_price   numeric(12,2) NOT NULL,
    cost_updated boolean NOT NULL DEFAULT false,
    cost_error   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_purchase_receipt_items_qty_check CHECK (quantity > 0),
    CONSTRAINT inventory_purchase_receipt_items_price_check CHECK (unit_price >= 0)
);

CREATE INDEX IF NOT EXISTS idx_inventory_purchase_receipt_items_receipt
    ON inventory_purchase_receipt_items(receipt_id);
-- 验收 6「后台可查某 SKU 的进货历史」：按 SKU / 变体取历史入库行。
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_receipt_items_sku
    ON inventory_purchase_receipt_items(project_id, sku_code, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_purchase_receipt_items_variant
    ON inventory_purchase_receipt_items(project_id, variant_id, created_at DESC);

COMMENT ON TABLE inventory_purchase_orders IS '采购单头（来源 = inventory_sources 货源，issue #18）';
COMMENT ON COLUMN inventory_purchase_orders.status IS '由「已入库数量 与 采购数量」推导：pending 未入库 / partial 部分入库 / received 已入库';
COMMENT ON TABLE inventory_purchase_order_lines IS '采购行：SKU × 采购数量 × 采购单价 × 已入库数量（原子递增，恒不超过采购数量）';
COMMENT ON TABLE inventory_purchase_receipts IS '入库单头：purchase 采购收货（必有采购单）/ production 自家工厂生产入库（无采购单，成本手工）';
COMMENT ON COLUMN inventory_purchase_receipts.request_id IS '幂等键：工程内唯一，重复提交命中既有入库单，不会重复入库';
COMMENT ON TABLE inventory_purchase_receipt_items IS '入库单行：数量与单价快照 + SKU 成本价回写结果';
