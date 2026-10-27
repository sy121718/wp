-- 670 · 采购单状态机补齐（B10）：pending → ordered，新增 draft / exception 两个人工态。
--
-- 背景：本仓采购单原先只有三个**推导态**（pending / partial / received，由
-- 「已入库数量 与 采购数量」算出）+ 两个终态（finished / cancelled，迁移 626）。
-- 它表达不了「还没正式下单」（草稿）与「这批货出问题了，先别收」（异常）——
-- 而这两件事都是运营每天要说的，只能写进备注，页面上筛不出来。
--
-- 对齐 dy_erp 的九个状态里本仓需要的七个：
--
--   draft     草稿        还没正式下单，行可自由改，**不可收货**
--   ordered   已下单      等收货（原 pending 改名）
--   partial   部分到货    有入库但没满
--   received  已到货      收满
--   exception 异常        人工标记，暂停收货，可解除
--   finished  已完结      提前关单（迁移 626）
--   cancelled 已取消      迁移 626
--
-- 为什么把 pending 改名成 ordered 而不是并存：两者是同一件事的两种说法
-- （「还没收到货」= 「已下单等收货」）。并存会让「筛选未入库」与「筛选已下单」
-- 命中同一批行，两个筛选项给出同一份结果 —— 这类重复维度最终一定会被误用成
-- 「两个不同的业务阶段」。改名由本迁移回填，应用层常量同步改（PurchaseStatusOrdered）。
--
-- 幂等：CREATE OR REPLACE FUNCTION 覆盖旧函数体；DROP CONSTRAINT 后再 ADD（ADD 用 DO 块
-- 吞 duplicate_object，重跑安全）；回填 UPDATE 带 WHERE 条件，重跑不改变任何行。

-- ── 1. 单头写入守卫：只对**推导态**强制重算，人工态一律放行 ────────────────
--
-- 196 的守卫无条件把 status 换成「明细行推导值」；626 给它加了「终态放行」。
-- 现在人工态从两个变成四个（draft / exception / finished / cancelled），
-- 再逐个列举就该写成「推导态才管」—— 判据换成推导态名单，新增人工态时不必再改这里。
CREATE OR REPLACE FUNCTION fn_inventory_purchase_order_status_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_status text;
BEGIN
    -- 人工态（draft / exception / finished / cancelled）：不按明细行改判。
    -- 草稿改行不该自动变成「已下单」；异常中的单收货会被服务层拒绝，
    -- 行数据也就不会变 —— 推导在这里没有意义，只会把人的决定抹掉。
    IF NEW.status NOT IN ('ordered', 'partial', 'received') THEN
        RETURN NEW;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'ordered'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'ordered'
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

-- ── 2. 明细行变化后的单头同步：同样只动推导态 ──────────────────────────────
--
-- 这条函数由 lines 表的 AFTER INSERT/UPDATE/DELETE 触发（196 建立、206 修过时间列名）。
-- 改动只有两处：推导值 pending → ordered；UPDATE 的 WHERE 加「单头当前是推导态」。
-- 后者是必须的：草稿单被加了一行明细（改单）不该因此变成「已下单」。
CREATE OR REPLACE FUNCTION fn_inventory_purchase_order_status_sync()
 RETURNS trigger
 LANGUAGE plpgsql
AS $$
DECLARE
    v_order_id uuid;
    v_status   text;
BEGIN
    v_order_id := COALESCE(NEW.order_id, OLD.order_id);
    IF v_order_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'ordered'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'ordered'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = v_order_id;

    UPDATE inventory_purchase_orders
       SET status = v_status, update_time = now()
     WHERE id = v_order_id
       AND status IN ('ordered', 'partial', 'received')
       AND status IS DISTINCT FROM v_status;

    RETURN NULL;
END;
$$;

-- ── 3. 约束：先拆旧的三态约束，回填，再装七态 ──────────────────────────────
--
-- 顺序不能反：库里还有 'pending' 行时直接 ADD 新 CHECK 会整表校验失败
-- （ADD CONSTRAINT 默认对已有行做验证）。所以是 拆 → 回填 → 装。
ALTER TABLE inventory_purchase_orders DROP CONSTRAINT IF EXISTS inventory_purchase_orders_status_check;

UPDATE inventory_purchase_orders SET status = 'ordered' WHERE status = 'pending';

DO $$
BEGIN
    ALTER TABLE inventory_purchase_orders
        ADD CONSTRAINT inventory_purchase_orders_status_check
        CHECK (status IN ('draft', 'ordered', 'partial', 'received', 'exception', 'finished', 'cancelled'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

COMMENT ON COLUMN inventory_purchase_orders.status IS
    'draft 草稿（未下单，不可收货）/ ordered 已下单待收货 / partial 部分到货 / received 已到货 / exception 异常（暂停收货，可解除）/ finished 提前关单 / cancelled 取消';

-- ── 4. 默认值：新建单默认「已下单」 ────────────────────────────────────────
--
-- 草稿是**显式选择**（建单时勾「存为草稿」）。默认值跟着改名一起走，
-- 否则绕过服务层的写入（测试夹具、运维 SQL）会落进一个已不被接受的状态。
ALTER TABLE inventory_purchase_orders ALTER COLUMN status SET DEFAULT 'ordered';
