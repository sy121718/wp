-- 109 · 采购单与入库权限点 + 超管策略（issue #18）。
-- 与 100/104/106 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403
--（与 072/077/078/079/100/104/106 同因）。条件只看本票自己的权限点
--（inventory:purchase_%），与 100 的宽匹配互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('inventory:purchase_list',       '采购单列表',   'inventory', '/api/inventory/purchase/list',       'GET'),
    ('inventory:purchase_get',        '采购单详情',   'inventory', '/api/inventory/purchase/get',        'GET'),
    ('inventory:purchase_create',     '新建采购单',   'inventory', '/api/inventory/purchase/create',     'POST'),
    ('inventory:purchase_update',     '修改采购单',   'inventory', '/api/inventory/purchase/update',     'POST'),
    ('inventory:purchase_receipt',    '采购收货入库', 'inventory', '/api/inventory/purchase/receipt',    'POST'),
    ('inventory:purchase_production', '生产入库',     'inventory', '/api/inventory/purchase/production', 'POST'),
    ('inventory:purchase_history',    '进货历史',     'inventory', '/api/inventory/purchase/history',    'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/inventory/purchase/list',       'GET',  'inventory:purchase_list'),
    ('/api/inventory/purchase/get',        'GET',  'inventory:purchase_get'),
    ('/api/inventory/purchase/create',     'POST', 'inventory:purchase_create'),
    ('/api/inventory/purchase/update',     'POST', 'inventory:purchase_update'),
    ('/api/inventory/purchase/receipt',    'POST', 'inventory:purchase_receipt'),
    ('/api/inventory/purchase/production', 'POST', 'inventory:purchase_production'),
    ('/api/inventory/purchase/history',    'GET',  'inventory:purchase_history')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
