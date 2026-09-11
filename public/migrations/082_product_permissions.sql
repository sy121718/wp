-- 082_product_permissions.sql — 商品域权限点与超管策略（issue #5 / T3a）。
-- 与 030/031/032/033 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。

-- 权限点（create_by/update_by 为 bigint 类型，用 0 对齐 030）。
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('product:create',         '商品创建',   'product', '/api/product/create',         'POST'),
    ('product:update',         '商品更新',   'product', '/api/product/update',         'POST'),
    ('product:get',            '商品详情',   'product', '/api/product/get',            'GET'),
    ('product:list',           '商品列表',   'product', '/api/product/list',           'GET'),
    ('product:delete',         '商品删除',   'product', '/api/product/delete',         'POST'),
    ('product:variant_create', '变体创建',   'product', '/api/product/variant/create', 'POST'),
    ('product:variant_update', '变体更新',   'product', '/api/product/variant/update', 'POST'),
    ('product:variant_delete', '变体删除',   'product', '/api/product/variant/delete', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/product/create',         'POST', 'product:create'),
    ('/api/product/update',         'POST', 'product:update'),
    ('/api/product/get',            'GET',  'product:get'),
    ('/api/product/list',           'GET',  'product:list'),
    ('/api/product/delete',         'POST', 'product:delete'),
    ('/api/product/variant/create', 'POST', 'product:variant_create'),
    ('/api/product/variant/update', 'POST', 'product:variant_update'),
    ('/api/product/variant/delete', 'POST', 'product:variant_delete')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);