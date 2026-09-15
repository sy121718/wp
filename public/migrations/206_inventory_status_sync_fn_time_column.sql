-- 206 · DB-019 连带修复：采购单状态同步触发器函数引用旧时间列名。
--
-- 205 把 created_at / updated_at 全量改名为 create_time / update_time，但
-- **函数体里的列引用不会随 RENAME COLUMN 自动更新** —— PG 只重写索引表达式、
-- 视图定义、约束这类可解析对象，plpgsql 函数体是字符串，改名后仍按旧名解析。
--
-- 实测代价：inventory_purchase_order_lines 写入触发 fn_inventory_purchase_order_status_sync
-- → 报 column "updated_at" of relation "inventory_purchase_orders" does not exist，
-- 采购收货整条链路（feature 测试 12 个用例）直接失败。
--
-- 全库排查：同一个查询扫过 pg_proc（prokind='f'）、pg_attrdef、pg_constraint（CHECK）、
-- information_schema.views、生成列表达式 —— 只有本函数命中，其余对象都由 PG 自动跟随。
--
-- CREATE OR REPLACE FUNCTION 不破坏已建立的触发器绑定，可重复执行。
-- 注册侧判定：函数定义里已无 updated_at（而不是「函数是否存在」）。
--
-- 分隔符必须用 $$：migrator.SplitStatements 只识别 $$ 这一种 dollar-quote 标签，
-- 写 $function$ 会被当成普通文本，语句在函数体中间被 ';' 切断（报
-- unterminated dollar-quoted string）。196 当年用的就是 $$。

CREATE OR REPLACE FUNCTION fn_inventory_purchase_order_status_sync()
 RETURNS trigger
 LANGUAGE plpgsql
AS $$
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

    -- 只在真的不一致时写：同一个值反复写会让 update_time 抖动，
    -- 也让每次收货都被记成一次「采购单本身被修改」。
    UPDATE inventory_purchase_orders
       SET status = v_status, update_time = now()
     WHERE id = v_order_id AND status IS DISTINCT FROM v_status;

    RETURN NULL;
END;
$$;
