-- 126 · 邮箱模块权限点与后台菜单（issue #37）。
-- 与 100/104/109 同构：权限点幂等 seed（WHERE NOT EXISTS），菜单（type=2）挂在
-- 「站点工程」目录下、按钮（type=3）挂在菜单下。未 seed 时后台侧栏看不到入口，
-- 页面本身仍可直接访问。

-- 1. 权限点
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'mail', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('mail:account_list',    '发信账号列表', 'mail', '/api/mail/account/list',   'GET'),
    ('mail:account_save',    '保存发信账号', 'mail', '/api/mail/account/save',   'POST'),
    ('mail:account_delete',  '删除发信账号', 'mail', '/api/mail/account/delete', 'POST'),
    ('mail:account_default', '设默认发信账号', 'mail', '/api/mail/account/default', 'POST'),
    ('mail:account_test',    '测试发送邮件', 'mail', '/api/mail/account/test',   'POST'),
    ('mail:template_list',   '邮件模板列表', 'mail', '/api/mail/template/list',  'GET'),
    ('mail:template_save',   '保存邮件模板', 'mail', '/api/mail/template/save',  'POST'),
    ('mail:template_delete', '删除邮件模板', 'mail', '/api/mail/template/delete', 'POST'),
    ('mail:contact_list',    '联系人列表',   'mail', '/api/mail/contact/list',   'GET'),
    ('mail:contact_import',  '导入联系人',   'mail', '/api/mail/contact/import', 'POST'),
    ('mail:contact_status',  '修改联系人状态', 'mail', '/api/mail/contact/status', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 2. 一级菜单「邮箱管理」
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '邮箱管理',
       -- 找不到父目录时落到顶级（0）：测试 schema 与精简部署都可能没有「系统设置」目录。
       COALESCE((SELECT id FROM sys_menus WHERE title = '系统设置' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/mail', 'view.mail', 'mail:account_list', 1, 20, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '邮箱管理' AND type = 2 AND deleted_at IS NULL);

-- 3. 菜单下的按钮权限
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = v.parent AND type = 2 AND deleted_at IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('邮箱管理', '新建 / 编辑账号', 3, 'mail:account_save', 60),
    ('邮箱管理', '删除账号',       3, 'mail:account_delete', 61),
    ('邮箱管理', '设默认账号',     3, 'mail:account_default', 62),
    ('邮箱管理', '测试发送',       3, 'mail:account_test', 63),
    ('邮箱管理', '编辑模板',       3, 'mail:template_save', 64),
    ('邮箱管理', '删除模板',       3, 'mail:template_delete', 65),
    ('邮箱管理', '导入联系人',     3, 'mail:contact_import', 66),
    ('邮箱管理', '修改联系人状态', 3, 'mail:contact_status', 67)
) AS v(parent, title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = v.title AND type = 3 AND permission_code = v.code AND deleted_at IS NULL);
