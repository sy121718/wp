-- 236 · 修正菜单 is_public 误置（库存目录 / 仓库管理 / 变动原因字典）
--
-- 既有约定（224_admin_nav_menu_rebuild.sql 第 167 行的注释）：
--   **只有仪表盘这类「无子节点的直接链接」才 is_public = 1** —— 它是「登录即可见」的开关
--   （权限码为空 / NULL 的菜单只有靠它才进得了授权树）。业务页面一律 is_public = 0，
--   可见性由权限码决定。
--
-- 229（库存从「商品与库存」下拆出成一级目录）给三个菜单写了 is_public = 1：
--   · 「库存」目录（type=1，parent_id=0）
--   · 「仓库管理」/admin/inventory/warehouses
--   · 「变动原因字典」/admin/inventory/reasons
-- 后果：这三个对**任何登录用户**都在侧栏可见，点进去 API 仍按权限 403 ——
-- 正是「看得到点不了」的反模式：用户会以为功能坏了，而不是「我没有这个权限」。
--
-- 目录的可见性不需要 is_public：授权树会在它有可见子孙时自动补齐它
-- （internal/module/admin/service/menu_authz.go 的 buildAuthorizedTree）。
-- 幂等：注册项 ConditionSQL 判「这三条是否都已归零」，已修则跳过。
UPDATE sys_menus
SET is_public = 0, update_time = NOW()
WHERE deleted_at IS NULL
  AND is_public = 1
  AND (
        path IN ('/admin/inventory/warehouses', '/admin/inventory/reasons')
        OR (type = 1 AND parent_id = 0 AND title = '库存')
      );
