-- 034_content_pipeline_permissions.sql — 内容模板与自动发布权限点（0-A2）。
-- contenttemplate + presentation 两模块，与 030~033 同构。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('contenttemplate:create', '模板创建', 'contenttemplate', '/api/contenttemplate/create', 'POST'),
    ('contenttemplate:update', '模板更新', 'contenttemplate', '/api/contenttemplate/update', 'POST'),
    ('contenttemplate:get',    '模板详情', 'contenttemplate', '/api/contenttemplate/get',    'GET'),
    ('contenttemplate:list',   '模板列表', 'contenttemplate', '/api/contenttemplate/list',   'GET'),
    ('presentation:create', '实例创建', 'presentation', '/api/presentation/create', 'POST'),
    ('presentation:rebuild', '实例重建', 'presentation', '/api/presentation/rebuild', 'POST'),
    ('presentation:get',    '实例详情', 'presentation', '/api/presentation/get',    'GET'),
    ('presentation:list',   '实例列表', 'presentation', '/api/presentation/list',   'GET'),
    ('presentation:delete', '实例删除', 'presentation', '/api/presentation/delete', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/contenttemplate/create', 'POST', 'contenttemplate:create'),
    ('/api/contenttemplate/update', 'POST', 'contenttemplate:update'),
    ('/api/contenttemplate/get',    'GET',  'contenttemplate:get'),
    ('/api/contenttemplate/list',   'GET',  'contenttemplate:list'),
    ('/api/presentation/create', 'POST', 'presentation:create'),
    ('/api/presentation/rebuild', 'POST', 'presentation:rebuild'),
    ('/api/presentation/get',    'GET',  'presentation:get'),
    ('/api/presentation/list',   'GET',  'presentation:list'),
    ('/api/presentation/delete', 'POST', 'presentation:delete')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
