-- 183 · 产物 SEO 体检权限点（审计 SEO-019）。
--
-- 单独一个权限点，与「改 URL」「发布」分开：体检是只读的诊断动作，
-- 能看体检报告的人未必有权改站点 —— 把诊断与变更分成两个权限点，
-- 才可能让运营看到问题、由开发去改。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('seo:audit', '产物 SEO 体检', 'seo', '/api/seo/audit', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/seo/audit', 'POST', 'seo:audit')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );
