-- 104 · 库存变动 / 流水 / 原因字典 / 物料清单 / 缓存对账权限点（issue #16）。
-- 与 100 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403
--（与 072/077/078/079/100 同因）。条件只看本票自己的权限点（inventory:stock_change
-- 等 8 个），与 100 的宽匹配互不干扰。
--
-- 2026-09 修正：本票原有 10 个权限点，其中 inventory:cache_sync / cache_reconcile 随库存
-- 缓存一起下线（迁移 122 负责从存量库删掉它们）。但把这两个码**留在本 seed 里**会把删除
-- 撤回：本 seed 的幂等条件是「本票的权限点齐了才跳过」，122 删掉 2 个之后条件立刻不满足
-- → 每次启动都把它们重新插回来。而且 **Migrations 台账先跑、Seeds 台账后跑**
--（migrator.go 的 runAll 与 RunSeeds 是两个循环），所以每轮启动的终态都是「死权限点又回来了」，
-- 表现为后台存在指向不存在路由的死授权（勾选后毫无作用、误导配置者）。
-- 兜底测试：internal 之外的 migrations_retired_permission_test.go（删能力必须连 seed 一起收口）。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('inventory:stock_change',      '按 SKU 增减库存',   'inventory', '/api/inventory/stock/change',       'POST'),
    ('inventory:stock_deduct',      '按 SKU 扣减库存',   'inventory', '/api/inventory/stock/deduct',       'POST'),
    ('inventory:movement_list',     '库存流水列表',      'inventory', '/api/inventory/movement/list',      'GET'),
    ('inventory:reason_list',       '变动原因列表',      'inventory', '/api/inventory/reason/list',        'GET'),
    ('inventory:reason_create',     '新建变动原因',      'inventory', '/api/inventory/reason/create',      'POST'),
    ('inventory:reason_update',     '修改变动原因',      'inventory', '/api/inventory/reason/update',      'POST'),
    ('inventory:bom_set',           '维护物料清单',      'inventory', '/api/inventory/bom/set',            'POST'),
    ('inventory:bom_get',           '查看物料清单',      'inventory', '/api/inventory/bom/get',            'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/inventory/stock/change',    'POST', 'inventory:stock_change'),
    ('/api/inventory/stock/deduct',    'POST', 'inventory:stock_deduct'),
    ('/api/inventory/movement/list',   'GET',  'inventory:movement_list'),
    ('/api/inventory/reason/list',     'GET',  'inventory:reason_list'),
    ('/api/inventory/reason/create',   'POST', 'inventory:reason_create'),
    ('/api/inventory/reason/update',   'POST', 'inventory:reason_update'),
    ('/api/inventory/bom/set',         'POST', 'inventory:bom_set'),
    ('/api/inventory/bom/get',         'GET',  'inventory:bom_get')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
