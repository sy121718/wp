-- 146 · 订单备注权限点 + 超管策略（BIZ-1）。
-- 与 136/139/142/145 同构：权限点 + 超管全量策略。
--
-- 备注是后台对订单的判断记录（客服 / 仓库写），**不改变订单状态**，
-- 因此它是独立权限点：能看订单的人不一定该能改备注，反之亦然。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('order:note', '订单备注', 'order', '/api/order/note', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES ('/api/order/note', 'POST', 'order:note')) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );
