-- 228 · 角色权限分配页（角色分权）的词条
--
-- 背景：/admin/roles/permissions 是本轮新增的页面 —— 在此之前 /api/role/menu/list 与
--   /api/role/menu/save 只有服务端实现（权限点、超管保护都在），但没有任何界面调用方，
--   「这个角色能用哪些菜单与按钮」实际上只能靠直接改库完成。
-- 归属：internal/templates/admin/role_permissions.html（页面正文与工具条）、
--   internal/module/admin/inbound/http/admin_pages_handle.go 的 pagesMsgRolePermissionsTitle（页面标题）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，不是真相来源）。
-- 判定限定在自己的 key 上：不数全库行数（存量库永远判定已灌满），也不按前缀模糊匹配
--   （别的批次会顺带满足它，「看起来已应用、词条却没写进去」）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('MsgRolePermissionsTitle', 'zh-CN', '角色权限分配', 200, 'admin', 'internal/module/admin/inbound/http', 1, now(), now()),
('MsgRolePermissionsTitle', 'en-US', 'Role permissions', 200, 'admin', 'internal/module/admin/inbound/http', 1, now(), now()),
('admin.roles.action.permissions', 'zh-CN', '权限分配', 200, 'admin', 'internal/templates/admin/roles.html', 1, now(), now()),
('admin.roles.action.permissions', 'en-US', 'Permissions', 200, 'admin', 'internal/templates/admin/roles.html', 1, now(), now()),
('admin.roles.perm.title', 'zh-CN', '权限分配：', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.title', 'en-US', 'Permissions: ', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.intro', 'zh-CN', '勾选该角色可用的目录、菜单与按钮。勾选子项会自动补齐它所属的菜单与目录（不补齐会出现「接口能调、侧栏却没有入口」的分裂授权）；取消父项会同时取消其下全部子项。保存按整棵树的当前勾选做全量替换。', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.intro', 'en-US', 'Tick the directories, menus and buttons this role may use. Ticking a child also ticks its parent menu and directory (otherwise the role can call the API but has no sidebar entry); unticking a parent untickes its whole subtree. Saving replaces the entire permission set with the current selection.', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.back', 'zh-CN', '返回角色列表', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.back', 'en-US', 'Back to roles', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.ph.search', 'zh-CN', '搜索菜单名称或权限码', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.ph.search', 'en-US', 'Search by menu name or permission code', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.all', 'zh-CN', '全选', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.all', 'en-US', 'Select all', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.none', 'zh-CN', '清空', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.none', 'en-US', 'Clear', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.expand', 'zh-CN', '展开全部', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.expand', 'en-US', 'Expand all', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.collapse', 'zh-CN', '折叠全部', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.collapse', 'en-US', 'Collapse all', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.hint.search', 'zh-CN', '搜索与折叠只影响显示，保存始终提交整棵树的勾选。', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.hint.search', 'en-US', 'Search and collapse only change what is shown; saving always submits the whole tree.', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.empty', 'zh-CN', '没有可分配的菜单，先去「菜单管理」建目录与菜单。', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.empty', 'en-US', 'No menus to assign yet. Create directories and menus under Menu Management first.', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.toggle', 'zh-CN', '展开 / 收起子项', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.toggle', 'en-US', 'Expand / collapse children', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.badge.disabled', 'zh-CN', '已禁用', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.badge.disabled', 'en-US', 'Disabled', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.save', 'zh-CN', '保存权限', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now()),
('admin.roles.perm.action.save', 'en-US', 'Save permissions', 200, 'admin', 'internal/templates/admin/role_permissions.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
