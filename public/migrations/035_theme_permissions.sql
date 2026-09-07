-- 035_theme_permissions.sql — 主题（Theme）权限点与超管策略。
--
-- 背景：主题管理此前无权限点体系——/api/theme/* 路由仅挂 SessionAuth，
-- 无 CSRF、无 Casbin；dashboard 的 /admin/themes 写操作同样绕过鉴权。
-- 本迁移补齐 project 模块下 theme 能力的权限点 + 超管全量策略，
-- 使 theme 路由与页面写操作可复用统一鉴权链。
--
-- 幂等：ConditionSQL 守卫（register.go 注册 Seed，RunSeeds 执行）。

-- 权限点（theme 归属 project 模块，code 前缀 project:theme_*）。
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('project:theme_list',     '主题列表',   'project', '/api/theme/list',     'GET'),
    ('project:theme_active',   '当前主题',   'project', '/api/theme/active',   'GET'),
    ('project:theme_create',   '新建主题',   'project', '/api/theme/create',   'POST'),
    ('project:theme_update',   '更新主题',   'project', '/api/theme/update',   'POST'),
    ('project:theme_activate', '激活主题',   'project', '/api/theme/activate', 'POST'),
    ('project:theme_delete',   '删除主题',   'project', '/api/theme/delete',   'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/theme/list',     'GET',  'project:theme_list'),
    ('/api/theme/active',   'GET',  'project:theme_active'),
    ('/api/theme/create',   'POST', 'project:theme_create'),
    ('/api/theme/update',   'POST', 'project:theme_update'),
    ('/api/theme/activate', 'POST', 'project:theme_activate'),
    ('/api/theme/delete',   'POST', 'project:theme_delete')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
