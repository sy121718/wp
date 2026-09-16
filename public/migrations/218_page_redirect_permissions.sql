-- ========================================
-- 218 · 重定向管理权限点、超管策略与后台菜单（审计 SEO-025）
--
-- 背景：改 URL 时可以勾选「保留旧链接」，系统会落一份 redirect.json 并激活到旧路径
--（SiteRedirectMiddleware 读它做 301）。但没有任何界面能看见这些重定向、也无法手动增删 ——
-- 改十次 URL 就积十条链，A 到 B、B 再到 C，没人知道线上到底有多少条、指向哪。本批补上管理页。
--
-- 为什么权限点必须与路由同批：页面与接口挂在 authorizedAPI 组下，该组统一挂
-- CasbinMiddleware()，按**实际请求路径** enforce —— 权限点缺失时没有任何策略能匹配，
-- **含超管在内全员 403**（072/077/078/079/151/213 各踩过一次）。超管策略同理，
-- 只补权限点不补策略，超管自己也点不动。
--
-- 4 个权限点与 4 条路由一一对应（sys_permission 按 (api_path, api_method) 唯一）：
--   GET  /api/page/redirect         页面（列出当前工程全部 301）
--   POST /api/page/redirect/create  手动新增（校验源空闲、目标存在、不成环）
--   POST /api/page/redirect/delete  删除（解激活 + 清占用账）
--   POST /api/page/redirect/merge   多跳链一键合并
--
-- 菜单挂在「站点工程」目录下（与 140 系统页面槽位同构）。path 写**真实可达路径**：
-- 本页挂在 /api 组下（page 模块在 routes.go 里只拿到 authorizedAPI 这个已装配好的组），
-- 写 /admin/xxx 会指到一个不存在的页面。
--
-- 幂等：三段各自带 NOT EXISTS 守卫（重复执行不报错、不产生重复行）。
-- 注册：public/migrations/register_admin_i18n.go（Seed 218-page-redirect-permissions）。
-- ========================================

-- 1) 权限点（4 条）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('page:redirect_view',   '重定向列表',   'page', '/api/page/redirect',        'GET'),
    ('page:redirect_create', '新增重定向',   'page', '/api/page/redirect/create', 'POST'),
    ('page:redirect_delete', '删除重定向',   'page', '/api/page/redirect/delete', 'POST'),
    ('page:redirect_merge',  '合并重定向链', 'page', '/api/page/redirect/merge',  'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);

-- 2) 超管策略（身份表全量超管 × 这 4 个权限点，缺哪条补哪条）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code LIKE 'page:redirect_%'
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );

-- 3) 后台菜单（二级菜单，绑定列表权限点）
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '重定向管理',
       COALESCE((SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/api/page/redirect', 'view.page.redirects', 'page:redirect_view', 1, 13, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '重定向管理' AND type = 2 AND deleted_at IS NULL);
