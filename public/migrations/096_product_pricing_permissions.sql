-- 096 · 定价工具权限点 + 超管策略（issue #13）。
-- 与 082/086a/087/089/092/030/031 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。
-- 条件只看本票自己的权限点（product:pricing_%），与 082 的宽匹配（product:%）互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('product:pricing_rules',      '定价规则类型', 'product', '/api/product/pricing/rules',      'GET'),
    ('product:pricing_roundings',  '定价尾数处理', 'product', '/api/product/pricing/roundings',  'GET'),
    ('product:pricing_preview',    '定价试算预览', 'product', '/api/product/pricing/preview',    'POST'),
    ('product:pricing_apply',      '定价应用落库', 'product', '/api/product/pricing/apply',      'POST'),
    ('product:pricing_history',    '定价留痕列表', 'product', '/api/product/pricing/history',    'GET'),
    ('product:pricing_adjustment', '定价留痕详情', 'product', '/api/product/pricing/adjustment', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/product/pricing/rules',      'GET',  'product:pricing_rules'),
    ('/api/product/pricing/roundings',  'GET',  'product:pricing_roundings'),
    ('/api/product/pricing/preview',    'POST', 'product:pricing_preview'),
    ('/api/product/pricing/apply',      'POST', 'product:pricing_apply'),
    ('/api/product/pricing/history',    'GET',  'product:pricing_history'),
    ('/api/product/pricing/adjustment', 'GET',  'product:pricing_adjustment')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
