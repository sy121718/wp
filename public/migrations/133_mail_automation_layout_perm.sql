-- 133 · 补自动化画布位置接口的权限点（issue #38 P4）。
--
-- 起因：P4 加了 POST /api/mail/automation/layout（保存画布位置），但只在路由里注册了路径，
-- 忘了建权限点 —— 点「保存位置」直接 403。这类「路由加了、权限点忘了」的缺陷
-- 单元测试与页面渲染测试都发现不了，只有真打一次接口才暴露。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'mail', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('mail:automation_layout', '保存画布位置', '/api/mail/automation/layout', 'POST')
) AS v(code, name, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 只加权限点还不够：超管策略（051）是「权限点全表的快照」，新增权限点不会自动补策略。
-- 051 的注释里记过这个坑（「权限点先落库则策略永远不补」），后果就是超管点按钮 403。
-- 所以这里把策略一并补上（CROSS JOIN 全部 is_admin=1 的管理员，与 051 同一手法）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code = 'mail:automation_layout'
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );

-- 菜单按钮（与其它自动化按钮同列）。
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '邮箱管理',
       COALESCE((SELECT id FROM sys_menus WHERE title = '邮箱管理' AND type = 2 AND deleted_time IS NULL), 0),
       3, '', '', 'mail:automation_layout', 1, 85, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '邮箱管理' AND type = 3 AND permission_code = 'mail:automation_layout' AND deleted_time IS NULL);
