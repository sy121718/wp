-- 导航写端点按动作拆开（一个端点一种确定协议）：为新增的三个端点补权限点与超管策略。
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('navigation:move',       '导航排序',     'navigation', '/api/navigation/move',         'POST'),
    ('navigation:translate',  '导航译文写入', 'navigation', '/api/navigation/translations', 'POST'),
    ('navigation:sourceAdd',  '来源加入导航', 'navigation', '/api/navigation/sources',      'POST'),
    ('navigation:deleteBulk', '导航批量删除', 'navigation', '/api/navigation/delete-bulk',  'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/navigation/move',         'POST', 'navigation:move'),
    ('/api/navigation/translations', 'POST', 'navigation:translate'),
    ('/api/navigation/sources',      'POST', 'navigation:sourceAdd'),
    ('/api/navigation/delete-bulk',  'POST', 'navigation:deleteBulk')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method AND r.v3 = t.code);
