-- 471 · 菜单绑定权限点（多对多）的新词条。
--
-- 背景：一个菜单节点从「只能绑一个 permission_code」升级为「可以挂多个码」（迁移 470 建表），
--   菜单管理页因此多了三样界面：列表的「权限点」列、新建 / 编辑抽屉里的复选清单、清单上的搜索框。
--
-- 归属：internal/templates/admin/system/menus.html、
--   internal/templates/admin/system/menu_perm_field.html（两个调用点共用）。
-- 复用而不新增：「清空」按钮与「已选 {n} 项」计数沿用既有词条
--   admin.roles.perm.action.none / admin.common.bulk.selected（迁移 230 已 seed），
--   本批不重复造 —— 同一句话在两处用两个 key，改文案时必然漏一处。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，不是真相来源）。
-- 判定限定在本批自己的 5 个 key 上：不数全库行数（存量库永远判定已灌满），
--   也不按前缀模糊匹配（别的批次会顺带满足它，「看起来已应用、词条却没写进去」）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.menus.col.permissions', 'zh-CN', '权限点', 200, 'admin', 'internal/templates/admin/system/menus.html: 列表列头', 1, now(), now()),
('admin.menus.col.permissions', 'en-US', 'Permissions', 200, 'admin', 'internal/templates/admin/system/menus.html: 列表列头', 1, now(), now()),
('admin.menus.field.permissions', 'zh-CN', '权限点', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 表单字段', 1, now(), now()),
('admin.menus.field.permissions', 'en-US', 'Permissions', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 表单字段', 1, now(), now()),
('admin.menus.field.permissions_hint', 'zh-CN', '菜单与按钮至少绑一个权限点，可以绑多个；目录 / iframe / 外链不允许绑。', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 规则说明', 1, now(), now()),
('admin.menus.field.permissions_hint', 'en-US', 'Menus and buttons need at least one permission and may bind several; directories, iframes and external links must not bind any.', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 规则说明', 1, now(), now()),
('admin.menus.field.permissions_search', 'zh-CN', '搜索权限码或权限名', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 搜索框', 1, now(), now()),
('admin.menus.field.permissions_search', 'en-US', 'Search code or name', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 搜索框', 1, now(), now()),
('admin.menus.field.permissions_empty', 'zh-CN', '没有匹配的权限点。', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 搜索空态', 1, now(), now()),
('admin.menus.field.permissions_empty', 'en-US', 'No matching permissions.', 200, 'admin', 'internal/templates/admin/system/menu_perm_field.html: 搜索空态', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
