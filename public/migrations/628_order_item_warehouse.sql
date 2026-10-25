-- 625 · 订单行记录发货仓：预占与扣减必须落在同一个仓（S2）。
--
-- 为什么要落列而不是发货时再解析：
--   预占发生在**下单**，扣减发生在**发货**，两处都要知道「这批货从哪个仓出」。
--   若两处各自解析（都回落到默认仓），中间一旦默认仓被改，预占的那批货和发货扣的那批
--   就是两批不同的货 —— 账面上「预占了 3 件却从另一个仓扣了 3 件」，永远对不上。
--   落列之后这一行的事实只有一个来源。
--
-- 为什么可空：存量订单没有这个事实，回填到默认仓等于伪造历史。
--   NULL = 历史行，发货时按默认仓解析并**回写**这一列（一次性固化）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + 索引 IF NOT EXISTS。

ALTER TABLE order_items ADD COLUMN IF NOT EXISTS warehouse_id uuid;
CREATE INDEX IF NOT EXISTS idx_order_items_warehouse
    ON order_items (warehouse_id) WHERE warehouse_id IS NOT NULL;

COMMENT ON COLUMN order_items.warehouse_id IS '发货仓（下单时解析并固化；NULL = 存量行，发货时解析后回写）';
