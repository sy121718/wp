-- 202 · DB-015 残余第三处：inventory_warehouses.status 的 DDL CHECK。
--
-- 背景：orders.status（迁移 165）与 blocks.kind（迁移 166）已有 DDL CHECK 兜底，仓库状态
-- 仍只有 Go 侧白名单（inventory/enums：active / disabled），非法值可以绕过服务层写入。
-- 本迁移把这一处补齐，DB-015 的三处枚举列至此全部有 DDL 层兜底。
--
-- 幂等：DROP CONSTRAINT IF EXISTS + ADD CONSTRAINT；存量预检不通过时整条迁移失败并给出明确原因，
-- 避免「加约束失败」被读成无关报错。
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM inventory_warehouses
        WHERE status NOT IN ('active', 'disabled')
    ) THEN
        RAISE EXCEPTION 'inventory_warehouses.status 存在非法存量值，请先清洗后再执行本迁移';
    END IF;
END $$;

ALTER TABLE inventory_warehouses DROP CONSTRAINT IF EXISTS inventory_warehouses_status_check;
ALTER TABLE inventory_warehouses
    ADD CONSTRAINT inventory_warehouses_status_check
    CHECK (status IN ('active', 'disabled'));
