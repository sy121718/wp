-- 472 · 管理员授权分配（角色抽屉 + 菜单权限抽屉）的界面词条。
--
-- 背景：管理员的「分配角色」（/api/admin/role/list|save）与「直接额外菜单权限」
--   （/api/admin/menu/list|save）两条链路此前没有任何界面调用方，只能直接改 sys_casbin_rule。
--   本批在管理员列表行上把两个抽屉接上，这一屏的文案归这里。
--
-- 归属：internal/templates/admin/system/admin_role_drawer.html、
--   internal/templates/admin/system/admin_menu_drawer.html，
--   以及列表行上的两个入口按钮（administrators.html）。
-- 复用而不新增（迁移 230 / 193 / 469 已 seed）：「取消」「关闭」、搜索、全选 / 清空 / 展开 / 折叠、
--   展开收起子项、空态与去菜单管理、禁用徽标、已选计数 —— 同一句话在两处用两个 key，
--   改文案时必然漏一处。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，不是真相来源）。
-- 判定枚举本批自己的 15 个 key（×2 语言 = 30 行）：不数全库行数（存量库永远判定已灌满），
--   也不按前缀模糊匹配（别的批次会顺带满足它，「看起来已应用、词条却没写进去」）。
--   偏差方向刻意选「宁可重跑」：漏 seed 会让中英成对判据（admin_group_f_i18n_test.go）直接变红。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.admins.role.action', 'zh-CN', '分配角色', 200, 'admin', 'internal/templates/admin/system/administrators.html: 行操作', 1, now(), now()),
('admin.admins.role.action', 'en-US', 'Assign roles', 200, 'admin', 'internal/templates/admin/system/administrators.html: 行操作', 1, now(), now()),
('admin.admins.role.title', 'zh-CN', '分配角色：', 200, 'admin', 'internal/templates/admin/system/administrators.html: 抽屉标题前缀', 1, now(), now()),
('admin.admins.role.title', 'en-US', 'Assign roles: ', 200, 'admin', 'internal/templates/admin/system/administrators.html: 抽屉标题前缀', 1, now(), now()),
('admin.admins.role.intro', 'zh-CN', '勾选该管理员拥有的角色。保存按当前勾选做全量替换：没勾的会被解除绑定。角色带来的权限在「菜单权限」抽屉里显示为「来自角色」且不可在那里取消。', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 说明', 1, now(), now()),
('admin.admins.role.intro', 'en-US', 'Tick the roles this administrator holds. Saving replaces the whole set: unticked roles are unbound. Permissions coming from roles show as "From role" in the menu permissions drawer and cannot be removed there.', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 说明', 1, now(), now()),
('admin.admins.role.empty', 'zh-CN', '还没有可分配的角色，先去「角色管理」建一个。', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 空态', 1, now(), now()),
('admin.admins.role.empty', 'en-US', 'No roles to assign yet. Create one in Roles first.', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 空态', 1, now(), now()),
('admin.admins.role.empty.action', 'zh-CN', '去角色管理', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 空态动作', 1, now(), now()),
('admin.admins.role.empty.action', 'en-US', 'Go to roles', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 空态动作', 1, now(), now()),
('admin.admins.role.missing', 'zh-CN', '未知角色', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 已绑定但角色表查不到的码', 1, now(), now()),
('admin.admins.role.missing', 'en-US', 'Unknown role', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 已绑定但角色表查不到的码', 1, now(), now()),
('admin.admins.role.save', 'zh-CN', '保存角色', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 提交', 1, now(), now()),
('admin.admins.role.save', 'en-US', 'Save roles', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 提交', 1, now(), now()),
('admin.admins.role.saved', 'zh-CN', '角色已保存', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 成功回执', 1, now(), now()),
('admin.admins.role.saved', 'en-US', 'Roles saved', 200, 'admin', 'internal/templates/admin/system/admin_role_drawer.html: 成功回执', 1, now(), now()),
('admin.admins.menu.action', 'zh-CN', '菜单权限', 200, 'admin', 'internal/templates/admin/system/administrators.html: 行操作', 1, now(), now()),
('admin.admins.menu.action', 'en-US', 'Menu permissions', 200, 'admin', 'internal/templates/admin/system/administrators.html: 行操作', 1, now(), now()),
('admin.admins.menu.title', 'zh-CN', '菜单权限：', 200, 'admin', 'internal/templates/admin/system/administrators.html: 抽屉标题前缀', 1, now(), now()),
('admin.admins.menu.title', 'en-US', 'Menu permissions: ', 200, 'admin', 'internal/templates/admin/system/administrators.html: 抽屉标题前缀', 1, now(), now()),
('admin.admins.menu.intro', 'zh-CN', '勾选该管理员额外需要的菜单入口（超出角色已有的部分）。标着「来自角色」的项由角色授权，在这里取消不会生效，要去角色里改。保存按整棵树的当前勾选做全量替换。', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 说明', 1, now(), now()),
('admin.admins.menu.intro', 'en-US', 'Tick the menu entries this administrator needs on top of what the roles already grant. Items marked "From role" come from roles and cannot be removed here — change the role instead. Saving replaces the whole ticked set.', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 说明', 1, now(), now()),
('admin.admins.menu.hint', 'zh-CN', '搜索与折叠只影响显示，保存始终提交整棵树的勾选（继承项不参与提交）。', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 搜索提示', 1, now(), now()),
('admin.admins.menu.hint', 'en-US', 'Search and collapsing only affect display; saving always submits every tick (inherited items are not submitted).', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 搜索提示', 1, now(), now()),
('admin.admins.menu.inherited', 'zh-CN', '来自角色', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 继承项徽标', 1, now(), now()),
('admin.admins.menu.inherited', 'en-US', 'From role', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 继承项徽标', 1, now(), now()),
('admin.admins.menu.save', 'zh-CN', '保存菜单权限', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 提交', 1, now(), now()),
('admin.admins.menu.save', 'en-US', 'Save menu permissions', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 提交', 1, now(), now()),
('admin.admins.menu.saved', 'zh-CN', '菜单权限已保存', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 成功回执', 1, now(), now()),
('admin.admins.menu.saved', 'en-US', 'Menu permissions saved', 200, 'admin', 'internal/templates/admin/system/admin_menu_drawer.html: 成功回执', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
