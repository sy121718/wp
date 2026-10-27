-- 653 · 履约状态回填（B7）：从现有 status + 「发过货没有」推导。
--
-- 推导规则（一条订单只落一个值，按下列优先顺序判）：
--
--   status = 'cancelled'                    → cancelled
--   status = 'refunded'                     → refunded
--   status = 'pending'                      → pending
--   status = 'completed'                    → received（客户已收货；退货结案另由退货流程推进）
--   status = 'paid' 且 有发货明细            → partial（部分发货，剩余仍在 paid —— 这是既有口径：
--                                              order_shipment.nextStatus 在还有剩余时把状态留在 paid）
--   status = 'paid' 且 无发货明细            → paid
--   status = 'shipped'                      → shipped
--   其它（认不出的历史取值）                  → exception（保持原列默认值，由人工确认）
--
-- 为什么 completed 落 received 而不是 return_done：completed 只说明订单走完了（钱货两清），
--   退货是否发生是另一条事实（order_returns 有它自己的状态机）。把 completed 一律算成
--   「退货完成」会把从没退过货的单统计进退货里。
--
-- 「有发货明细」判据与代码里 pendingShipRemainingSQL / SumShippedQuantity 同源：
--   明细求和 > 0，不看缓存列。
--
-- 幂等：只改默认值那一批（fulfillment_status = 'pending'），重复执行结果相同；
--   已由业务写入的值不会被回填覆盖。

UPDATE orders o
SET fulfillment_status = CASE
        WHEN o.status = 'cancelled' THEN 'cancelled'
        WHEN o.status = 'refunded' THEN 'refunded'
        WHEN o.status = 'pending' THEN 'pending'
        WHEN o.status = 'completed' THEN 'received'
        WHEN o.status = 'shipped' THEN 'shipped'
        WHEN o.status = 'paid' AND EXISTS (
            SELECT 1 FROM order_shipment_items si
            JOIN order_shipments sh ON sh.id = si.shipment_id
            WHERE sh.order_id = o.id
        ) THEN 'partial'
        WHEN o.status = 'paid' THEN 'paid'
        ELSE 'exception'
    END
WHERE o.fulfillment_status = 'pending';

-- 已完成 / 已发货 / 已退款 / 已取消的单必然不在默认值上：上面的 CASE 已覆盖，
-- 但若迁移重跑时这些单仍停在 pending（人手改过列），此处的兜底保持同一口径。
UPDATE orders o
SET fulfillment_status = 'received'
WHERE o.fulfillment_status = 'pending' AND o.status = 'completed';
