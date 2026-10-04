-- 515 · AI 模块的后台菜单入口（/admin/ai/providers）。
--
-- 背景：AI 配置页的模板与路由（inbound/http/ai_page.go + internal/routers/assembly.go 的接线）
--   都指到 /admin/ai/providers，但 `sys_menus` 里从来没有这个条目 —— 菜单数据**只从 SQL seed**
--   来（权限点由装配末尾 permission.SyncToDB 幂等 upsert，菜单不在其列，见 assembly.go 的业务权限
--   注释块），所以页面对了、侧栏里却点不到。
--
-- 形态完全照 498_system_settings_menu.sql（给已有后台页面补侧栏入口的既有范式）：
--   · 一条 type=2（菜单）行，permission_code 写 ai:provider_list（菜单可见性挂**查询类**权限点，
--     一码一路由，见 internal/permission/codes.go 的 ai 段）；
--   · 再补一条 sys_menu_permission 关联 —— 470 之后读路径只读 sys_menu_permission，
--     sys_menus.permission_code 只是「seed 兼容写入点」，由触发器单向补进新表；
--     触发器在**精简 schema / 既有测试库**里可能不存在，故这里显式补一次（两次都幂等）。
--
-- 父级分组 title 用 COALESCE 兜底：历史 seed 里 '站点'（498）与 '站点工程'（101/084/150）
--   两套并存，不确定目标库里是哪一套，两套都试、都没有则挂根（parent_id=0）。
--
-- 幂等：两条都以「本批自己的对象」为 NOT EXISTS 判据（path / (menu_id, permission_code)），
--   重复执行安全。本迁移带 TableName（见 register_ai_menu.go）：sys_menus 若不存在即整条跳过。

INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT 'AI 模型',
       COALESCE(
           (SELECT p.id FROM sys_menus p WHERE p.title = '站点' AND p.type = 1 AND p.deleted_at IS NULL),
           (SELECT p.id FROM sys_menus p WHERE p.title = '站点工程' AND p.type = 1 AND p.deleted_at IS NULL),
           0),
       2, '/admin/ai/providers', 'sparkles', 'ai:provider_list', 1, 9, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus m WHERE m.path = '/admin/ai/providers' AND m.deleted_at IS NULL
);

INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, 'ai:provider_list', NOW(), NOW()
  FROM sys_menus m
 WHERE m.path = '/admin/ai/providers'
   AND m.deleted_at IS NULL
   AND NOT EXISTS (
       SELECT 1 FROM sys_menu_permission mp
        WHERE mp.menu_id = m.id AND mp.permission_code = 'ai:provider_list'
   );

-- 会话页入口（/admin/ai/sessions）：与供应商页同批，菜单可见性挂 ai:session_list。
-- 与页面路由的口径一致（/admin/ai/sessions 的 Casbin obj 是 /api/ai/session/list）——
-- 进入页面后各写操作再按各自的 ai:session_* 权限点由路由层 Casbin 拦。
INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT 'AI 会话',
       COALESCE(
           (SELECT p.id FROM sys_menus p WHERE p.title = '站点' AND p.type = 1 AND p.deleted_at IS NULL),
           (SELECT p.id FROM sys_menus p WHERE p.title = '站点工程' AND p.type = 1 AND p.deleted_at IS NULL),
           0),
       2, '/admin/ai/sessions', 'chat', 'ai:session_list', 1, 10, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus m WHERE m.path = '/admin/ai/sessions' AND m.deleted_at IS NULL
);

INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, 'ai:session_list', NOW(), NOW()
  FROM sys_menus m
 WHERE m.path = '/admin/ai/sessions'
   AND m.deleted_at IS NULL
   AND NOT EXISTS (
       SELECT 1 FROM sys_menu_permission mp
        WHERE mp.menu_id = m.id AND mp.permission_code = 'ai:session_list'
   );
