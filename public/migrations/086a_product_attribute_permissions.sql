-- 086a · 商品属性组权限点 + 超管策略（issue #7）。
-- 与 082/030/031 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('product:attribute_list',        '属性组列表', 'product', '/api/product/attribute/list',       'GET'),
    ('product:attribute_get',         '属性组详情', 'product', '/api/product/attribute/get',        'GET'),
    ('product:attribute_create',      '属性组创建', 'product', '/api/product/attribute/create',     'POST'),
    ('product:attribute_update',      '属性组更新', 'product', '/api/product/attribute/update',     'POST'),
    ('product:attribute_delete',      '属性组删除', 'product', '/api/product/attribute/delete',     'POST'),
    ('product:attribute_set_values',  '属性值保存', 'product', '/api/product/attribute/set-values', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/product/attribute/list',       'GET',  'product:attribute_list'),
    ('/api/product/attribute/get',        'GET',  'product:attribute_get'),
    ('/api/product/attribute/create',     'POST', 'product:attribute_create'),
    ('/api/product/attribute/update',     'POST', 'product:attribute_update'),
    ('/api/product/attribute/delete',     'POST', 'product:attribute_delete'),
    ('/api/product/attribute/set-values', 'POST', 'product:attribute_set_values')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
