-- 178 · 文案词条管理的权限点与菜单（审计 I18N-003）。
--
-- 只有一个写权限点（i18n:manage → POST /api/i18n/save）：列表与筛选是只读的（GET 不挂 Casbin），
-- 保存与删除共用它 —— 两者是「改站点文案」这一件事的两种动作，拆成两个权限点只会让
-- 「给了保存、忘了给删除」这类配置错误有机会发生，而它的表现是「删除按钮点了没反应」。
--
-- 菜单挂「站点工程」目录：词条是站点级资源（组件文案、后台外壳都按站点取），
-- 与「访问统计」「客户管理」同一层。未 seed 时侧栏看不到入口，页面仍可直接访问 /admin/i18n。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('i18n:manage', '文案词条管理', 'i18n', '/api/i18n/save', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/i18n/save', 'POST', 'i18n:manage')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '文案词条',
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_time IS NULL), 0),
       2, '/i18n', 'view.i18n', 'i18n:manage', 1, 16, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '文案词条' AND type = 2 AND deleted_time IS NULL);
