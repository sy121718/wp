-- 145 · 退货入库权限点 + 超管策略（BIZ-1）。
-- 与 136/139/142 同构：权限点 + 超管全量策略。
--
-- 访客侧的申请 / 撤销不走 Casbin（访客没有权限点，能做的只有「操作自己的订单」），
-- 这里是**后台**的审核链路。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('order:return_list',    '退货申请列表', 'order', '/api/order/return/list',    'GET'),
    ('order:return_get',     '退货申请详情', 'order', '/api/order/return/get',     'GET'),
    ('order:return_approve', '同意退货',     'order', '/api/order/return/approve', 'POST'),
    ('order:return_reject',  '拒绝退货',     'order', '/api/order/return/reject',  'POST'),
    ('order:return_receive', '退货入库',     'order', '/api/order/return/receive', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/order/return/list',    'GET',  'order:return_list'),
    ('/api/order/return/get',     'GET',  'order:return_get'),
    ('/api/order/return/approve', 'POST', 'order:return_approve'),
    ('/api/order/return/reject',  'POST', 'order:return_reject'),
    ('/api/order/return/receive', 'POST', 'order:return_receive')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );
