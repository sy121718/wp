-- 623 · 出库扣减优先级 + 采购单终态（W2）。
--
-- 扣减优先级（对齐 ERP prd_warehouse.deduct_priority）：
--   只有在「出库 且 行 / 请求都没指定仓」时才启用 —— 显式指定的仓永远优先，
--   否则运营在表单里选的仓会被优先级悄悄改掉，那是比默认仓更坏的体验。
--   值越小越先扣；同值按仓短码、再按 id，保证跨环境可重现（确定性）。
--
--   为什么默认 100 而不是 0：0 会让「没设过优先级的仓」排在任何显式设过正数的仓之前，
--   也就是把「没配置」当成「最高优先级」。100 是一个中位数档：既不会被新仓抢到最前，
--   也不至于永远排在最后 —— 与「未设置」等价。
--
-- 采购终态：本地状态原由到货数量推导（pending / partial / received），无法表达
--   「提前关单」与「取消」。补 finished / cancelled 两个显式终态；前三个中间态的
--   推导逻辑不动，只在两个终态上改道。
--
--   为什么不给行数据加触发器来派生终态：终态是**人工决定**（提前关单意味着
--   「剩下的不再等」），不是行数据的函数 —— 让触发器从数量反推会把人工决定抹掉。
--
-- 幂等：ADD COLUMN IF NOT EXISTS；索引 IF NOT EXISTS；CHECK 先 DROP 后 ADD（吞 duplicate_object）。

ALTER TABLE inventory_warehouses ADD COLUMN IF NOT EXISTS deduct_priority integer NOT NULL DEFAULT 100;
COMMENT ON COLUMN inventory_warehouses.deduct_priority IS '出库扣减优先级：值小先扣；仅在未显式指定仓库时生效。默认 100 = 与未设置等价';

CREATE INDEX IF NOT EXISTS idx_inventory_warehouses_deduct
    ON inventory_warehouses (project_id, deduct_priority, code);

-- 采购单状态约束：原约束只认三态，先按名字 DROP 再加五态版本。
-- DROP IF EXISTS 对「约束不存在」是空操作；ADD 用 DO 块吞 duplicate_object（重跑安全）。
ALTER TABLE inventory_purchase_orders DROP CONSTRAINT IF EXISTS inventory_purchase_orders_status_check;
DO $$
BEGIN
    ALTER TABLE inventory_purchase_orders
        ADD CONSTRAINT inventory_purchase_orders_status_check
        CHECK (status IN ('pending', 'partial', 'received', 'finished', 'cancelled'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

COMMENT ON COLUMN inventory_purchase_orders.status IS 'pending 未入库 / partial 部分入库 / received 已收满 / finished 提前关单（剩余不再期待）/ cancelled 取消（仅未收满的单）';

-- ── 3. 单头写入守卫要让终态通行（迁移 196 的守卫在迁移 632 之后必须改）─────
--
-- 196 的 BEFORE UPDATE OF status 守卫无条件把 status 换成「明细行推导值」。那对三个
-- 推导态是对的，但它会把迁移 632 引入的两个**人工终态**（finished / cancelled）当场抹回
-- received / partial —— 表现为「关单接口返回成功、库里还是 received」。
--
-- 改法：只有当前值**不是**终态时才强制推导。终态是单向的：一旦人工关单 / 取消，
-- 明细行的变化（收货 / 退货）不得把它复活成推导态 —— 那正是「提前关单」这个决定的含义。
--
-- 幂等：CREATE OR REPLACE FUNCTION 覆盖旧函数体，DROP TRIGGER IF EXISTS + CREATE 重建。
CREATE OR REPLACE FUNCTION fn_inventory_purchase_order_status_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_status text;
BEGIN
    -- 已经是人工终态：不再按明细行改判（终态单向）。
    IF NEW.status IN ('finished', 'cancelled') THEN
        RETURN NEW;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'pending'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'pending'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = NEW.id;

    IF NEW.status IS DISTINCT FROM v_status THEN
        NEW.status := v_status;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_inventory_purchase_orders_status_guard ON inventory_purchase_orders;

CREATE TRIGGER trg_inventory_purchase_orders_status_guard
    BEFORE UPDATE OF status ON inventory_purchase_orders
    FOR EACH ROW EXECUTE FUNCTION fn_inventory_purchase_order_status_guard();
