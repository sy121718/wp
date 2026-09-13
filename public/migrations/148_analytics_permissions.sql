-- 148 · 访问统计权限点 + 超管策略（BIZ-8）。
-- 与 136/139/142/145/146 同构：权限点 + 超管全量策略。
--
-- 只有**一个只读权限点**：打点端点是公开路由（访客的浏览器直连，没有会话也没有权限点），
-- 后台也没有任何写入口 —— 计数是事实流水，能被改写的计数等于没有计数。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('analytics:view', '访问统计查看', 'analytics', '/api/analytics/summary', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/analytics/summary', 'GET', 'analytics:view')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );
