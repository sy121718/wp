-- 032_plugin_permissions.sql — 插件管理权限点与超管策略（docs/06-plugin-system.md）。
-- 与 030/031 同构：权限点 + 后台菜单 + 超管全量策略。
-- 幂等：ConditionSQL 守卫（register.go 注册，RunSeeds 执行）。

-- 权限点（create_by/update_by 为 bigint 类型，用 0 对齐 030）。
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('plugin:install',   '插件安装',   'plugin', '/api/plugin/install',   'POST'),
    ('plugin:list',      '插件列表',   'plugin', '/api/plugin/list',      'GET'),
    ('plugin:toggle',    '插件启停',   'plugin', '/api/plugin/toggle',    'POST'),
    ('plugin:uninstall', '插件卸载',   'plugin', '/api/plugin/uninstall', 'POST'),
    ('plugin:detail',    '插件详情',   'plugin', '/api/plugin/detail',    'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 后台菜单：插件管理（type 为 smallint：2=菜单；数字位对齐 030）。
INSERT INTO sys_menus (title, parent_id, type, path, component, external_url, icon, status, is_hidden, is_public, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '插件管理', 0, 2, '/admin/plugins', 'plugin/list', '', 'puzzle', 1, 0, 0, 1, 60, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/plugins');

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/plugin/install',   'POST', 'plugin:install'),
    ('/api/plugin/list',      'GET',  'plugin:list'),
    ('/api/plugin/toggle',    'POST', 'plugin:toggle'),
    ('/api/plugin/uninstall', 'POST', 'plugin:uninstall'),
    ('/api/plugin/detail',    'GET',  'plugin:detail')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
