-- 469 · 角色权限分配（抽屉形态）的成功回执词条。
--
-- 背景：角色权限分配从独立页面改成了角色列表行的抽屉（片段由
--   GET /admin/roles/permissions/drawer 取回）。原先的「保存成功」确认来自
--   「回到分配页、按库中真实策略重新渲染出勾选态」—— 那个页面没有了，抽屉需要自己
--   给一句就地回执，否则用户关掉抽屉后无从判断这次保存是否生效。
-- 归属：internal/templates/admin/system/role_permissions.html 的成功态分支。
--   「关闭」按钮复用既有的 shell.action.close（059 已 seed），本批不重复造。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，不是真相来源）。
-- 判定限定在自己的 key 上：不数全库行数（存量库永远判定已灌满），也不按前缀模糊匹配
--   （别的批次会顺带满足它，「看起来已应用、词条却没写进去」）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.roles.perm.saved', 'zh-CN', '权限已保存', 200, 'admin', 'internal/templates/admin/system/role_permissions.html', 1, now(), now()),
('admin.roles.perm.saved', 'en-US', 'Permissions saved', 200, 'admin', 'internal/templates/admin/system/role_permissions.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
