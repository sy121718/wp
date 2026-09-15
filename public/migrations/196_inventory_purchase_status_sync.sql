-- 196_inventory_purchase_status_sync.sql
--
-- 审计 DB-008：采购单 status 是应用维护的冗余列，可用生成列消除不一致风险。
--
-- ── 前提修正：生成列在这个语义下不成立 ──────────────────────────────────────
--
-- 审计前半句属实（status 确实由应用写回，见 model.UpdatePurchaseOrderStatusTx），
-- 但后半句的生成列落不了地：**status 不取决于本行的其它列，而取决于明细行**——
--   该单一行都没有            → pending
--   所有行 received = quantity → received
--   其余（有任何一行动过）      → partial
-- 规则真源在 internal/module/product/inventory/service/inventory_purchase.go 的
-- derivePurchaseStatus，108_inventory_purchase.sql 的表注释也把它钉成「推导值」。
-- PostgreSQL 的 GENERATED ALWAYS AS … STORED 只能引用**同一行**的列，不允许子查询、
-- 不允许跨行、不允许非 immutable 函数 —— 它拿不到「本单所有明细行的到货汇总」，
-- 所以生成列这条路径在这里是死的（换成虚拟生成列同样不行，限制一致）。
--
-- ── 落地的替代：触发器 ──────────────────────────────────────────────────────
--
-- 把同一条推导规则放进数据库：明细行的每次增删改之后重算单头状态，只在真的不一致时写回。
-- 应用侧因此不需要改动（service 仍在同一事务里自算并写回，两侧规则一致时结果相同），
-- 但「status 与明细行不一致」从此不再是应用层能单方面造成的事实 ——
-- 绕过服务层直接写明细行，数据库也会把单头拉回来。
--
-- 两道门合起来才是「数据库保证一致」：明细行变化后重算单头（AFTER），
-- 以及任何带 status 的单头写入落库前被换成推导值（BEFORE 守卫，挡住「直接改单头」这条缝）。
--
-- 并发安全：所有会动明细行的应用路径都先取采购单头的 FOR UPDATE 行锁
-- （model.LockPurchaseOrderTx），触发器在明细行写完之后更新的是**同一行**单头，
-- 锁序恒为「单头 → 明细行 → 单头（已持有）」，不引入新的等待环。
-- 级联删除（删单带走明细行）时触发器会 UPDATE 一行已在本事务删除的行，影响 0 行，无副作用。
--
-- 迁移器逐语句执行且不包事务（public/migrations/migrator.go）。因此本文件的形状是：
-- 先回填（此刻对象还没建，中途失败下次重跑整份迁移，不会留下「对象在、数据没回填」），
-- 对象全部幂等（CREATE OR REPLACE / DROP IF EXISTS + CREATE），
-- 触发器建好之后**再回填一次**，闭合「首次回填之后、触发器生效之前」这段窗口。

-- ── 1. 回填既有数据（幂等：只有真的不一致才写行）──────────────────────────
--
-- COUNT(l.id) 而不是 COUNT(*)：LEFT JOIN 下「一行明细都没有」会被 COUNT(*) 数成 1，
-- 那样空采购单会被误判成「全部收满」。这与服务层 derivePurchaseStatus 的空行分支逐字对应。
UPDATE inventory_purchase_orders AS o
   SET status = d.status, updated_at = now()
  FROM (
        SELECT o2.id AS order_id,
               CASE
                   WHEN COUNT(l.id) = 0 THEN 'pending'
                   WHEN COUNT(l.id) FILTER (WHERE l.received_quantity < l.quantity) = 0 THEN 'received'
                   WHEN COUNT(l.id) FILTER (WHERE l.received_quantity > 0) > 0 THEN 'partial'
                   ELSE 'pending'
               END AS status
          FROM inventory_purchase_orders AS o2
          LEFT JOIN inventory_purchase_order_lines AS l ON l.order_id = o2.id
         GROUP BY o2.id
       ) AS d
 WHERE o.id = d.order_id AND o.status IS DISTINCT FROM d.status;

-- ── 2. 推导规则落库（与 Go 的 derivePurchaseStatus 同一条规则、同一组取值）──
CREATE OR REPLACE FUNCTION fn_inventory_purchase_order_status_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_order_id uuid;
    v_status   text;
BEGIN
    -- INSERT 时有 NEW、DELETE 时有 OLD；另一侧为 NULL，COALESCE 取到的那一侧就是本单。
    v_order_id := COALESCE(NEW.order_id, OLD.order_id);
    IF v_order_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'pending'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'pending'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = v_order_id;

    -- 只在真的不一致时写：同一个值反复写会让 updated_at 抖动，
    -- 也让每次收货都被记成一次「采购单本身被修改」。
    UPDATE inventory_purchase_orders
       SET status = v_status, updated_at = now()
     WHERE id = v_order_id AND status IS DISTINCT FROM v_status;

    RETURN NULL;
END;
$$;

-- 不限定 UPDATE 的列：状态一致性是硬要求，为省一次索引范围内的聚合而漏掉
-- 「将来新增参与推导的列」不划算。明细行数上限 100（服务层 maxPurchaseLines）。
DROP TRIGGER IF EXISTS trg_inventory_purchase_lines_status_sync ON inventory_purchase_order_lines;

CREATE TRIGGER trg_inventory_purchase_lines_status_sync
    AFTER INSERT OR UPDATE OR DELETE ON inventory_purchase_order_lines
    FOR EACH ROW EXECUTE FUNCTION fn_inventory_purchase_order_status_sync();

-- ── 2b. 单头写入守卫：把「写歪的状态」挡在写入之前 ──────────────────────────
--
-- 上面那个触发器覆盖的是「明细行变了、单头没跟上」。还剩一条缝：有人**直接改写单头**
-- status（应用层写错、或者绕过服务层的写语句）—— 明细行没动，上面那个触发器不会醒。
-- 这道 BEFORE 守卫补上它：任何带 status 的单头写入，落库前都被换成明细行对应的推导值。
--
-- 它与应用层的写回不冲突：service 写的本就是同一条规则算出来的值，此时 NEW.status
-- 已经等于推导值，守卫不改动任何东西（下面那句 IS DISTINCT FROM 判断就是干这个的）。
-- 单头 UPDATE 的频率很低（改备注 / 改收货仓 / 收货时写状态），多一次索引范围内的聚合可忽略。
CREATE OR REPLACE FUNCTION fn_inventory_purchase_order_status_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_status text;
BEGIN
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

-- 只在语句里写了 status 的 UPDATE 上触发：改备注 / 改收货仓这类写入不受影响。
DROP TRIGGER IF EXISTS trg_inventory_purchase_orders_status_guard ON inventory_purchase_orders;

CREATE TRIGGER trg_inventory_purchase_orders_status_guard
    BEFORE UPDATE OF status ON inventory_purchase_orders
    FOR EACH ROW EXECUTE FUNCTION fn_inventory_purchase_order_status_guard();

-- ── 3. 触发器生效后再回填一次（闭合窗口，逻辑与第 1 段相同）────────────────
--
-- 迁移期间业务可能仍在写（advisory lock 只挡并发迁移，不挡业务）：
-- 首次回填之后、触发器建好之前落进来的明细行没有被任何一侧修正过。
UPDATE inventory_purchase_orders AS o
   SET status = d.status, updated_at = now()
  FROM (
        SELECT o2.id AS order_id,
               CASE
                   WHEN COUNT(l.id) = 0 THEN 'pending'
                   WHEN COUNT(l.id) FILTER (WHERE l.received_quantity < l.quantity) = 0 THEN 'received'
                   WHEN COUNT(l.id) FILTER (WHERE l.received_quantity > 0) > 0 THEN 'partial'
                   ELSE 'pending'
               END AS status
          FROM inventory_purchase_orders AS o2
          LEFT JOIN inventory_purchase_order_lines AS l ON l.order_id = o2.id
         GROUP BY o2.id
       ) AS d
 WHERE o.id = d.order_id AND o.status IS DISTINCT FROM d.status;

COMMENT ON COLUMN inventory_purchase_orders.status IS '由「已入库数量 与 采购数量」推导：pending 未入库 / partial 部分入库 / received 已入库（应用层推导 + 迁移 196 的触发器在数据库层兜底，两处规则一致）';
COMMENT ON FUNCTION fn_inventory_purchase_order_status_sync() IS '明细行增删改后重算所属采购单 status（审计 DB-008：生成列无法表达跨行推导，改用触发器）';
