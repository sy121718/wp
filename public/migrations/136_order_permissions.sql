-- 136 · 订单权限点 + 超管策略（BIZ-1 销售侧）。
-- 与 082/086a/087/089/092/096/100 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。
-- 条件只看本票自己的权限点（order:%），与其它模块的宽匹配互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('order:list',      '订单列表',   'order', '/api/order/list',      'GET'),
    ('order:get',       '订单详情',   'order', '/api/order/get',       'GET'),
    ('order:create',    '新建订单',   'order', '/api/order/create',    'POST'),
    ('order:status',    '订单状态流转', 'order', '/api/order/status',  'POST'),
    ('order:cancel',    '取消订单',   'order', '/api/order/cancel',    'POST'),
    ('order:refund',    '订单退款',   'order', '/api/order/refund',    'POST'),
    ('order:item_list', '订单项列表', 'order', '/api/order/item/list', 'GET'),
    ('order:log_list',  '状态流转流水', 'order', '/api/order/log/list', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/order/list',      'GET',  'order:list'),
    ('/api/order/get',       'GET',  'order:get'),
    ('/api/order/create',    'POST', 'order:create'),
    ('/api/order/status',    'POST', 'order:status'),
    ('/api/order/cancel',    'POST', 'order:cancel'),
    ('/api/order/refund',    'POST', 'order:refund'),
    ('/api/order/item/list', 'GET',  'order:item_list'),
    ('/api/order/log/list',  'GET',  'order:log_list')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
