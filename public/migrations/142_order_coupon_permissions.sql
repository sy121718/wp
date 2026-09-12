-- 142 · 优惠码权限点 + 超管策略（BIZ-1）。
-- 与 082/086a/087/089/092/096/100/136/139 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078/079 同因）。
-- 条件只看本票自己的权限点（order:coupon_%），与 order:% 的宽匹配互不干扰。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('order:coupon_list',       '优惠码列表',     'order', '/api/order/coupon/list',            'GET'),
    ('order:coupon_get',        '优惠码详情',     'order', '/api/order/coupon/get',             'GET'),
    ('order:coupon_create',     '新建优惠码',     'order', '/api/order/coupon/create',          'POST'),
    ('order:coupon_update',     '修改优惠码',     'order', '/api/order/coupon/update',          'POST'),
    ('order:coupon_delete',     '删除优惠码',     'order', '/api/order/coupon/delete',          'POST'),
    ('order:coupon_validate',   '优惠码试算',     'order', '/api/order/coupon/validate',        'GET'),
    ('order:coupon_redemption', '优惠码核销记录', 'order', '/api/order/coupon/redemption/list', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/order/coupon/list',            'GET',  'order:coupon_list'),
    ('/api/order/coupon/get',             'GET',  'order:coupon_get'),
    ('/api/order/coupon/create',          'POST', 'order:coupon_create'),
    ('/api/order/coupon/update',          'POST', 'order:coupon_update'),
    ('/api/order/coupon/delete',          'POST', 'order:coupon_delete'),
    ('/api/order/coupon/validate',        'GET',  'order:coupon_validate'),
    ('/api/order/coupon/redemption/list', 'GET',  'order:coupon_redemption')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );
