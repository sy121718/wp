-- 103 · 内置变动原因字典 seed（issue #16）。
--
-- 「变动原因覆盖出 / 入 / 调整各枚举」：三类方向都给出可直接用的内置原因，
-- 让「新增一次入库」不必先自己建字典；自定义原因（工程内 code 唯一）由
-- POST /api/inventory/reason/create 维护，一律走字典引用，不接受自由文本。
--
-- project_id 为 NULL 表示内置（全工程可见，不随工程创建而复制）；
-- 幂等条件 = 「每个内置 code 都已存在」，因此新增内置原因后重启即补齐。

INSERT INTO inventory_change_reasons (project_id, code, name, direction, is_builtin, status, sort)
SELECT NULL, v.code, v.name, v.direction, true, 'active', v.sort
FROM (VALUES
    ('purchase_in',    '采购入库', 'in',     10),
    ('return_in',      '退货入库', 'in',     20),
    ('transfer_in',    '调拨入库', 'in',     30),
    ('production_in',  '生产入库', 'in',     40),
    ('sale_out',       '销售出库', 'out',    10),
    ('damage_out',     '报损出库', 'out',    20),
    ('transfer_out',   '调拨出库', 'out',    30),
    ('stocktake_adjust','盘点调整','adjust', 10),
    ('manual_adjust',  '手工调整', 'adjust', 20)
) AS v(code, name, direction, sort)
WHERE NOT EXISTS (
    SELECT 1 FROM inventory_change_reasons r
    WHERE r.project_id IS NULL AND lower(r.code) = lower(v.code)
);
