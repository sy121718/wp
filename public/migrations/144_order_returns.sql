-- 144 · 退货申请与退货明细（BIZ-1，退货入库）。
--
-- 为什么需要独立的两张表（而不是复用取消 + 退款）：
--   · 取消是「货还没出去」，退货是「货已经出去了、又回来了」—— 后者有**实物验收**环节，
--     退款与入库可以不同步（客户寄回、仓库点货、财务退款是三个人的事）；
--   · 它是**部分退货**能表达的前提：按订单项记录数量，而不是把整单打成一个状态。
--
-- 三个设计决定：
--   1. 金额与商品信息全部**快照**（订单项改名改价不改写历史退货单）；
--   2. 「已退多少」不存冗余列，由明细聚合算出 —— 冗余计数总有一天会与明细对不上，
--      而对不上的时候没人知道该信哪一个；
--   3. 幂等键落在唯一索引上（同一工程同一 request_id 只落一张申请单）。

CREATE TABLE IF NOT EXISTS order_returns (
    id              BIGSERIAL PRIMARY KEY,
    project_id      UUID         NOT NULL,
    order_id        BIGINT       NOT NULL,
    -- 订单号快照：退货单要能独立读出来（客服报单号时不依赖 orders 表当前状态）。
    order_no        VARCHAR(40)  NOT NULL DEFAULT '',
    return_no       VARCHAR(40)  NOT NULL,
    -- requested 客户已申请 / approved 管理员同意待收货 / received 已入库待退款
    -- / completed 已完成 / rejected 已拒绝 / cancelled 客户撤销
    status          VARCHAR(20)  NOT NULL DEFAULT 'requested',
    reason          VARCHAR(255) NOT NULL DEFAULT '',
    refund_amount   BIGINT       NOT NULL DEFAULT 0,
    user_id         BIGINT,
    customer_email  VARCHAR(120) NOT NULL DEFAULT '',
    customer_name   VARCHAR(60)  NOT NULL DEFAULT '',
    -- admin_note 审核意见（拒绝原因 / 收货备注）。
    admin_note      VARCHAR(500) NOT NULL DEFAULT '',
    reviewer_id     BIGINT       NOT NULL DEFAULT 0,
    reviewer_name   VARCHAR(60)  NOT NULL DEFAULT '',
    reviewed_at     TIMESTAMP(3),
    received_at     TIMESTAMP(3),
    refunded_at     TIMESTAMP(3),
    transaction_id  VARCHAR(120) NOT NULL DEFAULT '',
    -- 客户侧的幂等键（前端生成一次、重试复用）。
    request_id      VARCHAR(64)  NOT NULL DEFAULT '',
    create_time     TIMESTAMP(3) NOT NULL DEFAULT NOW(),
    update_time     TIMESTAMP(3) NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_order_returns_status CHECK (status IN ('requested', 'approved', 'received', 'completed', 'rejected', 'cancelled')),
    CONSTRAINT ck_order_returns_amount CHECK (refund_amount >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_order_returns_no ON order_returns (project_id, return_no);
-- 幂等：同一个请求键只落一张申请单（空串不参与，允许客户多次申请不同批次）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_order_returns_request ON order_returns (project_id, request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS idx_order_returns_order ON order_returns (order_id);
CREATE INDEX IF NOT EXISTS idx_order_returns_project_status ON order_returns (project_id, status);
CREATE INDEX IF NOT EXISTS idx_order_returns_user ON order_returns (user_id);

CREATE TABLE IF NOT EXISTS order_return_items (
    id                BIGSERIAL PRIMARY KEY,
    return_id         BIGINT      NOT NULL,
    order_item_id     BIGINT      NOT NULL,
    -- 商品快照（与 order_items 同口径：退货单不依赖订单项当前的样子）。
    -- 商品引用**不可空**：退货一定来自某个订单项，而订单项一定有商品。
    product_id        UUID        NOT NULL,
    variant_id        UUID        NOT NULL,
    product_name      VARCHAR(200) NOT NULL DEFAULT '',
    variant_label     VARCHAR(200) NOT NULL DEFAULT '',
    sku               VARCHAR(80)  NOT NULL DEFAULT '',
    unit_price        BIGINT      NOT NULL DEFAULT 0,
    quantity          INT         NOT NULL,
    -- 实际入库数量：首版等于申请数量（确认收货时一次收齐），
    -- 保留该列是为了将来支持「少件 / 破损折价」时不必再改表结构与聚合口径。
    received_quantity INT         NOT NULL DEFAULT 0,
    refund_amount     BIGINT      NOT NULL DEFAULT 0,
    create_time       TIMESTAMP(3) NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_order_return_items_qty CHECK (quantity > 0 AND received_quantity >= 0 AND received_quantity <= quantity),
    CONSTRAINT ck_order_return_items_amount CHECK (refund_amount >= 0 AND unit_price >= 0)
);

CREATE INDEX IF NOT EXISTS idx_order_return_items_return ON order_return_items (return_id);
-- 「这个订单项已经退过多少」的聚合走这条（累计不可超退的唯一依据）。
CREATE INDEX IF NOT EXISTS idx_order_return_items_order_item ON order_return_items (order_item_id);
