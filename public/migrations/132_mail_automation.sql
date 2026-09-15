-- 132 · 自动化流程权限点与菜单按钮（issue #38 P3）。
--
-- 挂在已有的「邮箱管理」菜单下（与账号 / 模板 / 联系人 / 活动同一处），
-- 不新开一级菜单 —— 自动化是邮件营销的一部分，不是独立功能域。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'mail', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('mail:automation_list',       '自动化流程列表',     '/api/mail/automation/list',         'GET'),
    ('mail:automation_get',        '自动化流程详情',     '/api/mail/automation/get',          'GET'),
    ('mail:automation_save',       '保存自动化流程',     '/api/mail/automation/save',         'POST'),
    ('mail:automation_status',     '启停自动化流程',     '/api/mail/automation/status',       'POST'),
    ('mail:automation_delete',     '删除自动化流程',     '/api/mail/automation/delete',       'POST'),
    ('mail:automation_start',      '把联系人加入流程',   '/api/mail/automation/start',        'POST'),
    ('mail:automation_run_list',   '自动化实例列表',     '/api/mail/automation/run/list',     'GET'),
    ('mail:automation_run_detail', '自动化实例排障详情', '/api/mail/automation/run/detail',   'GET'),
    ('mail:automation_tick',       '补投延时实例',       '/api/mail/automation/tick',         'POST')
) AS v(code, name, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = '邮箱管理' AND type = 2 AND deleted_at IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('邮箱管理', '保存流程',     3, 'mail:automation_save',   80),
    ('邮箱管理', '启停流程',     3, 'mail:automation_status', 81),
    ('邮箱管理', '删除流程',     3, 'mail:automation_delete', 82),
    ('邮箱管理', '加入流程',     3, 'mail:automation_start',  83),
    ('邮箱管理', '补投延时实例', 3, 'mail:automation_tick',   84)
) AS v(parent, title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = v.title AND type = 3 AND permission_code = v.code AND deleted_at IS NULL);
