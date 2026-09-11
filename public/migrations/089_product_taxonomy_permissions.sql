-- 089 · 商品分类与品牌权限点 + 超管策略（issue #10）。
-- 与 082/086a/087/030/031 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。
-- 条件只看本票自己的权限点（product:category_% / product:brand_%），
-- 与 082 的宽匹配（product:%）互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('product:category_list',   '商品分类列表', 'product', '/api/product/category/list',   'GET'),
    ('product:category_get',    '商品分类详情', 'product', '/api/product/category/get',    'GET'),
    ('product:category_create', '商品分类创建', 'product', '/api/product/category/create', 'POST'),
    ('product:category_update', '商品分类更新', 'product', '/api/product/category/update', 'POST'),
    ('product:category_delete', '商品分类删除', 'product', '/api/product/category/delete', 'POST'),
    ('product:brand_list',      '商品品牌列表', 'product', '/api/product/brand/list',      'GET'),
    ('product:brand_get',       '商品品牌详情', 'product', '/api/product/brand/get',       'GET'),
    ('product:brand_create',    '商品品牌创建', 'product', '/api/product/brand/create',    'POST'),
    ('product:brand_update',    '商品品牌更新', 'product', '/api/product/brand/update',    'POST'),
    ('product:brand_delete',    '商品品牌删除', 'product', '/api/product/brand/delete',    'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/product/category/list',   'GET',  'product:category_list'),
    ('/api/product/category/get',    'GET',  'product:category_get'),
    ('/api/product/category/create', 'POST', 'product:category_create'),
    ('/api/product/category/update', 'POST', 'product:category_update'),
    ('/api/product/category/delete', 'POST', 'product:category_delete'),
    ('/api/product/brand/list',      'GET',  'product:brand_list'),
    ('/api/product/brand/get',       'GET',  'product:brand_get'),
    ('/api/product/brand/create',    'POST', 'product:brand_create'),
    ('/api/product/brand/update',    'POST', 'product:brand_update'),
    ('/api/product/brand/delete',    'POST', 'product:brand_delete')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
