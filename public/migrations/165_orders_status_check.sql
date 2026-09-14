-- 165_orders_status_check.sql — DB-015：orders.status DDL CHECK（仅订单状态，存量预检后补约束）。

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM orders
        WHERE status NOT IN ('pending', 'paid', 'shipped', 'completed', 'cancelled', 'refunded')
    ) THEN
        RAISE EXCEPTION 'orders.status 存在非法存量值，请先清洗后再执行本迁移';
    END IF;
END $$;

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;

ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending', 'paid', 'shipped', 'completed', 'cancelled', 'refunded'));
