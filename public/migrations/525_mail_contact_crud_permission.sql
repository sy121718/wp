-- 525 · 联系人 CRUD 与标签管理的权限点 + 菜单按钮
--
-- 背景（issue #37 后续）：联系人此前只有 列表 / 导入 / 改状态 —— 建一条、改一条、
--   删一条、批量贴标签都要绕到数据库。补齐入口的前提是先有权限点与菜单按钮：
--   写操作的路由挂 builtin.CasbinMiddlewareForPath("/api/mail/contact/…")，
--   权限点不存在时**含超管在内全员 403**（AGENTS.md「新增 authorizedAPI 接口」）。
--
-- 本迁移做两件事：
--   1) 三个权限点：mail:contact_save / mail:contact_delete / mail:contact_tag，
--      api_path 与路由注册处的路径**完全同源**（页面 POST 回跳走同一个 path）；
--   2) 三个 type=3 按钮挂到「联系人」菜单（521 建，path = /admin/mail/contacts）下：
--      126 的按钮行挂在「邮箱管理」菜单下，本批属于联系人页自己的动作，父节点不同、
--      互不影响；sort 68/69/70 在「联系人」菜单下是全新的（该菜单此前没有按钮行）。
--
-- 批量端点复用单条权限点（/mail/contacts/bulk-delete → /api/mail/contact/delete，
--   /mail/contacts/bulk-tag → /api/mail/contact/tag）：批量与单条在同一个页面里，
--   分列两个权限点会造出「能删一条但不能批量删」这种没有意义的状态。
--
-- 幂等：注册见 register_mail_contact_crud_permission.go。判据枚举本批自己的六个对象
--   （3 权限点 + 3 按钮），不用 LIKE 前缀、不用全库总量 —— 两类真实故障都发生过：
--   前缀会把别的批次的行算进来导致本批被静默跳过（058），全库总量会随新键增长而永远
--   追不平、每次启动都重跑（076）。SQL 本身也带 NOT EXISTS 保护，门槛失效时重放不插重复行。

-- 1) 权限点（三件套：code / name / module / api_path / api_method）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'mail', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('mail:contact_save',   '保存联系人', 'mail', '/api/mail/contact/save',   'POST'),
    ('mail:contact_delete', '删除联系人', 'mail', '/api/mail/contact/delete', 'POST'),
    ('mail:contact_tag',    '批量打标签', 'mail', '/api/mail/contact/tag',    'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 2) 「联系人」菜单下的按钮（type=3）
--
--    父菜单用 path 定位而不是 title —— 后台可能存在同名的其它菜单行，path 是页面身份的
--    唯一键。JOIN（而不是 COALESCE(..., 0)）保证父菜单缺失时**一行都不插**：
--    挂到 id=0 的孤儿按钮会在菜单树里飘着，且下次修好父菜单也不会自动归位。
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title, p.id, 3, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('保存联系人', 'mail:contact_save',   68),
    ('删除联系人', 'mail:contact_delete', 69),
    ('批量打标签', 'mail:contact_tag',    70)
) AS v(title, code, sort)
JOIN sys_menus p ON p.path = '/admin/mail/contacts' AND p.type = 2 AND p.deleted_at IS NULL
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus x
    WHERE x.permission_code = v.code AND x.type = 3 AND x.deleted_at IS NULL
);

-- 回滚（手工，无自动回滚）：
--   DELETE FROM sys_menus WHERE type = 3 AND permission_code IN
--     ('mail:contact_save', 'mail:contact_delete', 'mail:contact_tag');
--   DELETE FROM sys_permission WHERE permission_code IN
--     ('mail:contact_save', 'mail:contact_delete', 'mail:contact_tag');
