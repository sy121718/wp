-- 100 · 仓库与库存权限点 + 超管策略（issue #15）。
-- 与 082/086a/087/089/092/096 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。
-- 条件只看本票自己的权限点（inventory:%），与其它模块的宽匹配互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('inventory:warehouse_list',   '仓库列表',     'inventory', '/api/inventory/warehouse/list',   'GET'),
    ('inventory:warehouse_get',    '仓库详情',     'inventory', '/api/inventory/warehouse/get',    'GET'),
    ('inventory:warehouse_create', '新建仓库',     'inventory', '/api/inventory/warehouse/create', 'POST'),
    ('inventory:warehouse_update', '修改仓库',     'inventory', '/api/inventory/warehouse/update', 'POST'),
    ('inventory:warehouse_delete', '删除仓库',     'inventory', '/api/inventory/warehouse/delete', 'POST'),
    ('inventory:stock_list',       '库存记录列表', 'inventory', '/api/inventory/stock/list',       'GET'),
    ('inventory:stock_sku',        '某 SKU 各仓库存', 'inventory', '/api/inventory/stock/sku',     'GET'),
    ('inventory:stock_get',        '库存记录详情', 'inventory', '/api/inventory/stock/get',        'GET'),
    ('inventory:stock_ensure',     '库存记录生成', 'inventory', '/api/inventory/stock/ensure',     'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/inventory/warehouse/list',   'GET',  'inventory:warehouse_list'),
    ('/api/inventory/warehouse/get',    'GET',  'inventory:warehouse_get'),
    ('/api/inventory/warehouse/create', 'POST', 'inventory:warehouse_create'),
    ('/api/inventory/warehouse/update', 'POST', 'inventory:warehouse_update'),
    ('/api/inventory/warehouse/delete', 'POST', 'inventory:warehouse_delete'),
    ('/api/inventory/stock/list',       'GET',  'inventory:stock_list'),
    ('/api/inventory/stock/sku',        'GET',  'inventory:stock_sku'),
    ('/api/inventory/stock/get',        'GET',  'inventory:stock_get'),
    ('/api/inventory/stock/ensure',     'POST', 'inventory:stock_ensure')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
