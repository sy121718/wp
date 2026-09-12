-- 139 · 系统页面槽位权限点（BIZ-1）。
-- 与 082/086a/087/089/092/096/100/136 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('page:site_slot_list',   '系统页面槽位列表', 'page', '/api/page/site-slot/list',   'GET'),
    ('page:site_slot_bind',   '绑定系统页面',     'page', '/api/page/site-slot/bind',   'POST'),
    ('page:site_slot_unbind', '解绑系统页面',     'page', '/api/page/site-slot/unbind', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/page/site-slot/list',   'GET',  'page:site_slot_list'),
    ('/api/page/site-slot/bind',   'POST', 'page:site_slot_bind'),
    ('/api/page/site-slot/unbind', 'POST', 'page:site_slot_unbind')
) AS t(path, method, code)
WHERE NOT EXISTS (
    SELECT 1 FROM sys_casbin_rule r
    WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
);
