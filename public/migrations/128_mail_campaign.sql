-- 128 · 群发活动权限点与菜单按钮（issue #37）。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'mail', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('mail:campaign_list',   '群发活动列表', 'mail', '/api/mail/campaign/list',   'GET'),
    ('mail:campaign_get',    '群发活动详情', 'mail', '/api/mail/campaign/get',    'GET'),
    ('mail:campaign_save',   '保存群发活动', 'mail', '/api/mail/campaign/save',   'POST'),
    ('mail:campaign_delete', '删除群发活动', 'mail', '/api/mail/campaign/delete', 'POST'),
    ('mail:campaign_start',  '启动群发活动', 'mail', '/api/mail/campaign/start',  'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((SELECT id FROM sys_menus WHERE title = '邮箱管理' AND type = 2 AND deleted_time IS NULL), 0),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('邮箱管理', '新建 / 编辑活动', 3, 'mail:campaign_save', 70),
    ('邮箱管理', '删除活动',       3, 'mail:campaign_delete', 71),
    ('邮箱管理', '启动群发',       3, 'mail:campaign_start', 72)
) AS v(parent, title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = v.title AND type = 3 AND permission_code = v.code AND deleted_time IS NULL);
