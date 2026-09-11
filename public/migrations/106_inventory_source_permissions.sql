-- 106 · 货源管理权限点 + 超管策略（issue #17）。
-- 与 100/104 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403
--（与 072/077/078/079/100/104 同因）。条件只看本票自己的权限点（inventory:source_%），
-- 与 100 的宽匹配互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('inventory:source_list',    '货源列表',       'inventory', '/api/inventory/source/list',    'GET'),
    ('inventory:source_get',     '货源详情',       'inventory', '/api/inventory/source/get',     'GET'),
    ('inventory:source_create',  '新建货源',       'inventory', '/api/inventory/source/create',  'POST'),
    ('inventory:source_update',  '修改货源',       'inventory', '/api/inventory/source/update',  'POST'),
    ('inventory:source_delete',  '删除货源',       'inventory', '/api/inventory/source/delete',  'POST'),
    ('inventory:source_summary', '货源关联方统计', 'inventory', '/api/inventory/source/summary', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/inventory/source/list',    'GET',  'inventory:source_list'),
    ('/api/inventory/source/get',     'GET',  'inventory:source_get'),
    ('/api/inventory/source/create',  'POST', 'inventory:source_create'),
    ('/api/inventory/source/update',  'POST', 'inventory:source_update'),
    ('/api/inventory/source/delete',  'POST', 'inventory:source_delete'),
    ('/api/inventory/source/summary', 'GET',  'inventory:source_summary')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
