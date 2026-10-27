-- 652 · 订单履约状态独立成列（B7）：把「货」的语义从 status 里分出来。
--
-- 为什么新增一列而不是扩展现有 status 枚举：
--   status 承担的是**钱**的语义（pending 待付款 / paid 已付款 / cancelled 已取消 /
--   refunded 已退款），流转边由 allowedTransitions 判定、被退款/取消两个用例独占；
--   履约承担的是**货**的语义（部分发货 / 换货补发 / 已退货 / 退货完成 / 异常）。
--   硬塞进一个枚举会把两种正交的事实压成一条线：一张「已付款、部分发货、其中一行异常」
--   的单只能落一个值，另一个事实就丢了 —— 而仓库侧恰恰要按后者筛。
--   同批的迁移 627 注释里也写过「刻意不加 fulfill_status」：那时没有仓库作业台的需求，
--   现在有了，并由本列承载。
--
-- 与迁移 135 的历史取值兼容：status 的六态一个不动，状态机与 allowedTransitions 原样保留；
--   本列默认 pending，由 653 按 (status, 是否发过货) 回填。
--
-- 取值（对齐 dy_erp 的 11 态，去掉 go_wp 没有实体支撑的几个）：
--   pending       未付款/刚建单（货还没有动作）
--   paid          已付款待发货
--   shipped       已全部发出
--   partial       部分发货（还有未发数量）
--   received      客户已收货（= status completed 之后的履约终态）
--   exception     异常（发货失败 / 库存对不上，需要人工介入）
--   cancelled     已取消
--   reship        换货补发
--   returned      已退货（货已回仓）
--   return_done   退货完成（已退款并结案）
--   refunded      已退款
--   failed        履约失败（终态，与 exception 的区别：exception 可继续处理）
--
-- 索引：仓库作业台按 (project_id, fulfillment_status) 筛，与 status 的索引同形。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + 约束/索引各自判定。

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS fulfillment_status text NOT NULL DEFAULT 'pending';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('orders') AND conname = 'ck_orders_fulfillment_status'
    ) THEN
        ALTER TABLE orders ADD CONSTRAINT ck_orders_fulfillment_status
            CHECK (fulfillment_status IN ('pending', 'paid', 'shipped', 'partial', 'received',
                                          'exception', 'cancelled', 'reship', 'returned',
                                          'return_done', 'refunded', 'failed'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_orders_project_fulfillment
    ON orders (project_id, fulfillment_status);

COMMENT ON COLUMN orders.fulfillment_status IS
    '履约状态（货的语义，独立于 status 的钱的语义；B7 迁移 652）';
