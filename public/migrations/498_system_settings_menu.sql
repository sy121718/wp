-- 498 · 系统设置页（/admin/system）的侧栏入口。
--
-- 背景：本批上一个迁移（497）建了 trade 配置组、sysconfig 模块也注册了页面路由与权限点，
--   但 **sys_menus 里没有这一条** —— 页面有路由、有权限点、侧栏没入口，只能手输 URL 到达。
--   （同类问题 224 处理过一次：当时漏了十几个「页面确实存在」的菜单。）
--
-- 归属：挂在「站点」分组（id 由 title 解析，不写死 id —— 224 的写法），紧随「站点设置」
--   之后（sort_order 5，当前该组已用到 4，13 是重定向管理）。
--
-- 权限码用 sysconfig:get（本页的读权限，与路由注册处声明的一致）：NULL 权限码谁都匹配不上，
--   切表后它会直接消失 —— 224 记过的故障。
--   与 470 的机制配套：sys_menus.permission_code 是「seed 兼容写入点」，由触发器单向补进
--   sys_menu_permission，读路径只读新表，因此这里只写旧列即可。
--
-- 幂等：WHERE NOT EXISTS 按 path 判定；重复执行不动任何行。
INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '系统设置',
       COALESCE((SELECT p.id FROM sys_menus p WHERE p.title = '站点' AND p.type = 1 AND p.deleted_at IS NULL), 0),
       2, '/admin/system', 'settings', 'sysconfig:get', 1, 5, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus m WHERE m.path = '/admin/system' AND m.deleted_at IS NULL
);

-- 若上一次执行时触发器未建（旧库），补一次关联表登记（幂等）。
INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, 'sysconfig:get', NOW(), NOW()
  FROM sys_menus m
 WHERE m.path = '/admin/system' AND m.deleted_at IS NULL
   AND NOT EXISTS (
       SELECT 1 FROM sys_menu_permission mp WHERE mp.menu_id = m.id AND mp.permission_code = 'sysconfig:get'
   );
