-- 033_content_permissions.sql — CMS 内容管理权限点与超管策略（0-A2 content 模块）。
-- 与 030/031/032 同构：权限点 + 超管全量策略。

-- 权限点（create_by/update_by 为 bigint 类型，用 0 对齐 030）。
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('content:create', '内容创建', 'content', '/api/content/create', 'POST'),
    ('content:update', '内容更新', 'content', '/api/content/update', 'POST'),
    ('content:get',    '内容详情', 'content', '/api/content/get',    'GET'),
    ('content:list',   '内容列表', 'content', '/api/content/list',   'GET'),
    ('content:delete', '内容删除', 'content', '/api/content/delete', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/content/create', 'POST', 'content:create'),
    ('/api/content/update', 'POST', 'content:update'),
    ('/api/content/get',    'GET',  'content:get'),
    ('/api/content/list',   'GET',  'content:list'),
    ('/api/content/delete', 'POST', 'content:delete')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
