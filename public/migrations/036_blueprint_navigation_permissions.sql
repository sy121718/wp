-- 036_blueprint_navigation_permissions.sql — Blueprint 与 Navigation 权限点（0-B/0-C）。
-- 与 030~034 同构。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('blueprint:create',  '模板创建', 'blueprint', '/api/blueprint/create',  'POST'),
    ('blueprint:update',  '模板更新', 'blueprint', '/api/blueprint/update',  'POST'),
    ('blueprint:publish', '模板发布', 'blueprint', '/api/blueprint/publish', 'POST'),
    ('blueprint:get',     '模板详情', 'blueprint', '/api/blueprint/get',     'GET'),
    ('blueprint:list',    '模板列表', 'blueprint', '/api/blueprint/list',    'GET'),
    ('blueprint:delete',  '模板删除', 'blueprint', '/api/blueprint/delete',  'POST'),
    ('blueprint:init',    '初始化页面', 'blueprint', '/api/blueprint/init',  'GET'),
    ('navigation:create', '导航创建', 'navigation', '/api/navigation/create', 'POST'),
    ('navigation:update', '导航更新', 'navigation', '/api/navigation/update', 'POST'),
    ('navigation:get',    '导航详情', 'navigation', '/api/navigation/get',    'GET'),
    ('navigation:list',   '导航列表', 'navigation', '/api/navigation/list',   'GET'),
    ('navigation:delete', '导航删除', 'navigation', '/api/navigation/delete', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/blueprint/create',  'POST', 'blueprint:create'),
    ('/api/blueprint/update',  'POST', 'blueprint:update'),
    ('/api/blueprint/publish', 'POST', 'blueprint:publish'),
    ('/api/blueprint/get',     'GET',  'blueprint:get'),
    ('/api/blueprint/list',    'GET',  'blueprint:list'),
    ('/api/blueprint/delete',  'POST', 'blueprint:delete'),
    ('/api/blueprint/init',    'GET',  'blueprint:init'),
    ('/api/navigation/create', 'POST', 'navigation:create'),
    ('/api/navigation/update', 'POST', 'navigation:update'),
    ('/api/navigation/get',    'GET',  'navigation:get'),
    ('/api/navigation/list',   'GET',  'navigation:list'),
    ('/api/navigation/delete', 'POST', 'navigation:delete')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
