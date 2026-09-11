-- 115 · 捆绑品权限点 + 超管策略（issue #20）。
-- 与 092/096 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403
--（与 072/077/078/079/100/104/106/109/112 同因）。
-- 条件只看本票自己的权限点（product:bundle_%），与 082 的宽匹配（product:%）互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('product:bundle_get',      '捆绑配置读取',   'product', '/api/product/bundle/get',      'GET'),
    ('product:bundle_set',      '捆绑配置保存',   'product', '/api/product/bundle/set',      'POST'),
    ('product:bundle_validate', '捆绑整单校验',   'product', '/api/product/bundle/validate', 'POST'),
    ('product:bundle_skus',     '捆绑可选 SKU',   'product', '/api/product/bundle/skus',     'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/product/bundle/get',      'GET',  'product:bundle_get'),
    ('/api/product/bundle/set',      'POST', 'product:bundle_set'),
    ('/api/product/bundle/validate', 'POST', 'product:bundle_validate'),
    ('/api/product/bundle/skus',     'GET',  'product:bundle_skus')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
