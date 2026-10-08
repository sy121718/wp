-- 589 · 按钮码：给全部 type=3 节点补上「菜单侧的稳定标识」（模板据此判断按钮显隐）。
--
-- 背景（docs/02-Z §4.4）：模板里 145 处 `isset(.PermSet["权限码"])` 直接写权限码，
-- 于是「菜单」与「权限」两个概念在模板层混在一起 —— 改模板的人会以为自己在配权限。
-- 目标是把模板的词汇收进菜单体系：模板写 `{{if isset(.Buttons["<按钮码>"])}}`。
--
-- 本迁移做数据侧的准备工作，两件事：
--   1. 给**已有的 68 个** type=3 节点填 `title_key` = 它绑定权限码的 slug 形式
--      （`product:variant_create` → `product.variant_create`）；
--   2. 补 **56 个** type=3 节点 —— 模板里用到、却没有任何节点承载的权限码。
--      它们的 `title` 取 `sys_permission.permission_name`（授权界面上给运营看的那个名字，
--      不另编一套中文），父菜单取「该模块的列表类菜单」（见下表 parent_code），
--      这样它们在「菜单管理」里落在正确的位置。
--
-- 为什么放 `title_key` 而不是新增一列：这一列本来就是「节点的稳定标识」——
-- 菜单节点上它是标题的 i18n key，按钮节点上它是模板引用的按钮码。两者同义：
-- 「这个节点的 key」。加一列会让同一件事有两个存放点。
--
-- 幂等：ConditionSQL 判「没有哪个 type=3 节点缺 title_key」，命中即跳过。
-- 偏差方向刻意取「宁可重跑」：将来新增按钮节点忘了填码时，本批会再跑一次把它补上
-- （INSERT 有 NOT EXISTS 守卫，重跑是安全的），而不是静默留一个模板查不到的码。
--
-- 注册见 register_menu_button_code.go。

-- 1) 已有按钮节点补码（一个节点只绑一个码，slug 无歧义）。
UPDATE sys_menus m
   SET title_key = replace(mp.permission_code, ':', '.'), update_time = NOW()
  FROM sys_menu_permission mp
 WHERE mp.menu_id = m.id
   AND m.deleted_at IS NULL
   AND m.type = 3
   AND (m.title_key IS NULL OR m.title_key = '');

-- 2) 补缺口的 56 个节点。父菜单用标量子查询取唯一一条（parent_code 在 type=2 里唯一）。
WITH v(code, slug, parent_code) AS (VALUES
        ('admin:create', 'admin.create', 'admin:list'),
        ('admin:delete', 'admin.delete', 'admin:list'),
        ('admin:edit', 'admin.edit', 'admin:list'),
        ('admin:menu_list', 'admin.menu_list', 'admin:list'),
        ('admin:role_list', 'admin.role_list', 'admin:list'),
        ('ai:chat', 'ai.chat', 'ai:provider_list'),
        ('ai:provider_delete', 'ai.provider_delete', 'ai:provider_list'),
        ('ai:provider_models_fetch', 'ai.provider_models_fetch', 'ai:provider_list'),
        ('ai:provider_models_restore', 'ai.provider_models_restore', 'ai:provider_list'),
        ('ai:provider_models_save', 'ai.provider_models_save', 'ai:provider_list'),
        ('ai:provider_save', 'ai.provider_save', 'ai:provider_list'),
        ('ai:provider_status', 'ai.provider_status', 'ai:provider_list'),
        ('ai:session_append', 'ai.session_append', 'ai:provider_list'),
        ('ai:session_archive', 'ai.session_archive', 'ai:provider_list'),
        ('ai:session_fold', 'ai.session_fold', 'ai:provider_list'),
        ('ai:session_rename', 'ai.session_rename', 'ai:provider_list'),
        ('comment:review', 'comment.review', 'comment:list'),
        ('content:create', 'content.create', 'content:list'),
        ('contenttemplate:create', 'contenttemplate.create', 'contenttemplate:list'),
        ('datarule:create', 'datarule.create', 'datarule:list'),
        ('datarule:delete', 'datarule.delete', 'datarule:list'),
        ('datarule:update', 'datarule.update', 'datarule:list'),
        ('dept:create', 'dept.create', 'dept:list'),
        ('dept:delete', 'dept.delete', 'dept:list'),
        ('dept:update', 'dept.update', 'dept:list'),
        ('i18n:manage', 'i18n.manage', 'i18n:view'),
        ('inventory:reason_create', 'inventory.reason_create', 'inventory:reason_list'),
        ('inventory:reason_update', 'inventory.reason_update', 'inventory:reason_list'),
        ('membership:assign_set', 'membership.assign_set', 'membership:assign_list'),
        ('membership:assign_unlock', 'membership.assign_unlock', 'membership:assign_list'),
        ('membership:tier_create', 'membership.tier_create', 'membership:tier_list'),
        ('membership:tier_delete', 'membership.tier_delete', 'membership:tier_list'),
        ('membership:tier_update', 'membership.tier_update', 'membership:tier_list'),
        ('menu:create', 'menu.create', 'menu:list'),
        ('menu:delete', 'menu.delete', 'menu:list'),
        ('menu:update', 'menu.update', 'menu:list'),
        ('navigation:delete', 'navigation.delete', 'navigation:list'),
        ('navigation:update', 'navigation.update', 'navigation:list'),
        ('order:coupon_create', 'order.coupon_create', 'order:coupon_list'),
        ('order:coupon_update', 'order.coupon_update', 'order:coupon_list'),
        ('order:create', 'order.create', 'order:list'),
        ('page:delete', 'page.delete', 'page:list'),
        ('permission:create', 'permission.create', 'permission:list'),
        ('permission:delete', 'permission.delete', 'permission:list'),
        ('permission:update', 'permission.update', 'permission:list'),
        ('product:create', 'product.create', 'product:list'),
        ('product:delete', 'product.delete', 'product:list'),
        ('product:update', 'product.update', 'product:list'),
        ('product:variant_create', 'product.variant_create', 'product:list'),
        ('product:variant_generate', 'product.variant_generate', 'product:list'),
        ('project:theme_create', 'project.theme_create', 'project:theme_list'),
        ('role:create', 'role.create', 'role:list'),
        ('role:delete', 'role.delete', 'role:list'),
        ('role:menu_list', 'role.menu_list', 'role:list'),
        ('role:update', 'role.update', 'role:list'),
        ('seo:audit', 'seo.audit', 'seo:audit')
)
INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, title_key,
                       is_system, is_hidden, sort_order, create_by, create_time, update_by, update_time)
SELECT COALESCE(sp.permission_name, v.code),
       COALESCE((SELECT pm.id FROM sys_menus pm
                   JOIN sys_menu_permission mp ON mp.menu_id = pm.id
                  WHERE pm.deleted_at IS NULL AND pm.type = 2
                    AND mp.permission_code = v.parent_code
                  ORDER BY pm.id LIMIT 1), 0),
       3, '', '', v.code, v.slug, 1, 0,
       10 + ROW_NUMBER() OVER (PARTITION BY v.parent_code ORDER BY v.code),
       0, NOW(), 0, NOW()
  FROM v
  LEFT JOIN sys_permission sp ON sp.permission_code = v.code
 WHERE NOT EXISTS (
       SELECT 1 FROM sys_menus x
        WHERE x.deleted_at IS NULL AND x.type = 3 AND x.title_key = v.slug
   );

-- 3) 新节点同样要进关联表（读路径只读 sys_menu_permission，见迁移 470）。
INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, m.permission_code, NOW(), NOW()
  FROM sys_menus m
 WHERE m.deleted_at IS NULL
   AND m.type = 3
   AND m.title_key IS NOT NULL
   AND m.title_key <> ''
   AND m.permission_code IS NOT NULL
   AND m.permission_code <> ''
   AND NOT EXISTS (
       SELECT 1 FROM sys_menu_permission mp
        WHERE mp.menu_id = m.id AND mp.permission_code = m.permission_code
   );
