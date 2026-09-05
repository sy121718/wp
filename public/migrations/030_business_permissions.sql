-- ========================================
-- go_wp — 业务模块权限点定义（page / project / block / media / artifact / publication）
--
-- 内容：
--   1) sys_permission：六模块 29 个权限点（permission_code + module + api_path + api_method）
--   2) sys_menus：站点工程目录（type=1）+ 六模块菜单（type=2）+ 按钮（type=3）
--      菜单/按钮均绑定 permission_code，对齐「菜单即权限的可视化」约定
--
-- 幂等：每条 INSERT 均带 NOT EXISTS 守卫，重复执行不报错、不重复插入。
-- 注册：public/migrations/register.go（Seed 030-business-permissions），
-- 执行：随 internal/routers/routes.go 路由装配调用 migrations.RunSeeds。
-- ========================================

-- 1. 权限点（module 与 api_path/api_method 一一对应业务路由，禁止 RESTful 路径参数）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('page:list',               '页面列表',            'page',        '/api/page/list',                 'GET'),
    ('page:detail',             '页面详情',            'page',        '/api/page/detail',               'GET'),
    ('page:create',             '新建页面',            'page',        '/api/page/create',               'POST'),
    ('page:draft_save',         '保存草稿',            'page',        '/api/page/draft/save',           'POST'),
    ('page:revision_list',      '修订记录',            'page',        '/api/page/revision/list',        'GET'),
    ('page:build',              '构建页面',            'page',        '/api/page/build',                'POST'),
    ('page:publish',            '发布页面',            'page',        '/api/page/publish',              'POST'),
    ('page:rollback',           '回滚页面',            'page',        '/api/page/rollback',             'POST'),
    ('page:url_update',         '更新页面URL',         'page',        '/api/page/url/update',           'POST'),
    ('project:list',            '项目列表',            'project',     '/api/project/list',              'GET'),
    ('project:detail',          '项目详情',            'project',     '/api/project/detail',            'GET'),
    ('project:create',          '新建项目',            'project',     '/api/project/create',            'POST'),
    ('project:update',          '更新项目',            'project',     '/api/project/update',            'POST'),
    ('block:list',              '区块列表',            'block',       '/api/block/list',                'GET'),
    ('block:detail',            '区块详情',            'block',       '/api/block/detail',              'GET'),
    ('block:create',            '新建区块',            'block',       '/api/block/create',              'POST'),
    ('block:update',            '更新区块',            'block',       '/api/block/update',              'POST'),
    ('block:delete',            '删除区块',            'block',       '/api/block/delete',              'POST'),
    ('media:list',              '媒体列表',            'media',       '/api/media/list',                'GET'),
    ('media:detail',            '媒体详情',            'media',       '/api/media/detail',              'GET'),
    ('media:upload',            '上传媒体',            'media',       '/api/media/upload',              'POST'),
    ('media:update',            '更新媒体',            'media',       '/api/media/update',              'POST'),
    ('media:delete',            '删除媒体',            'media',       '/api/media/delete',              'POST'),
    ('media:category_tree',     '媒体分类树',          'media',       '/api/media/category/tree',       'GET'),
    ('media:category_create',   '新建媒体分类',        'media',       '/api/media/category/create',     'POST'),
    ('media:category_update',   '更新媒体分类',        'media',       '/api/media/category/update',     'POST'),
    ('media:category_delete',   '删除媒体分类',        'media',       '/api/media/category/delete',     'POST'),
    ('artifact:detail',         '构建产物详情',        'artifact',    '/api/artifact/detail',           'GET'),
    ('publication:receipts_pending', '待处理发布回执', 'publication', '/api/publication/receipts/pending', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission x WHERE x.permission_code = v.code);

-- 2. 目录：站点工程（type=1，不绑定权限）
-- 注意：init_schema.sql 的 sys_menus 无 title_key 列（与 GORM 模型不同步的历史遗留），
-- 本 seed 按实际表结构写入，不含 title_key。
INSERT INTO sys_menus (title, parent_id, type, path, component, external_url, icon, status, is_hidden, is_public, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '站点工程', 0, 1, '/project', '', '', 'i-ep:set-up', 1, 0, 0, 1, 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_time IS NULL);

-- 3. 菜单（type=2，绑定模块列表权限，component 对齐 soybean view.xxx 约定）
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       (SELECT id FROM sys_menus WHERE title = '站点工程' AND type = 1 AND deleted_time IS NULL),
       v.type, v.path, v.component, v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('项目管理',   2, '/project',      'view.project',      'project:list',             1),
    ('页面管理',   2, '/page',         'view.page',         'page:list',                2),
    ('区块管理',   2, '/block',        'view.block',        'block:list',               3),
    ('媒体管理',   2, '/media',        'view.media',        'media:list',               4),
    ('构建产物',   2, '/artifact',     'view.artifact',     'artifact:detail',          5),
    ('发布管理',   2, '/publication',  'view.publication',  'publication:receipts_pending', 6)
) AS v(title, type, path, component, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.title = v.title AND x.type = v.type AND x.deleted_time IS NULL);

-- 4. 按钮（type=3，绑定写/读权限，parent 指向所属菜单）
INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       (SELECT id FROM sys_menus WHERE title = v.parent_title AND type = 2 AND deleted_time IS NULL),
       v.type, '', '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('新建项目',     '项目管理', 3, 'project:create',          10),
    ('更新项目',     '项目管理', 3, 'project:update',          11),
    ('项目详情',     '项目管理', 3, 'project:detail',          12),
    ('新建页面',     '页面管理', 3, 'page:create',             10),
    ('页面详情',     '页面管理', 3, 'page:detail',             11),
    ('保存草稿',     '页面管理', 3, 'page:draft_save',         12),
    ('修订记录',     '页面管理', 3, 'page:revision_list',      13),
    ('构建页面',     '页面管理', 3, 'page:build',              14),
    ('发布页面',     '页面管理', 3, 'page:publish',            15),
    ('回滚页面',     '页面管理', 3, 'page:rollback',           16),
    ('更新页面URL',  '页面管理', 3, 'page:url_update',         17),
    ('新建区块',     '区块管理', 3, 'block:create',            10),
    ('区块详情',     '区块管理', 3, 'block:detail',            11),
    ('更新区块',     '区块管理', 3, 'block:update',            12),
    ('删除区块',     '区块管理', 3, 'block:delete',            13),
    ('上传媒体',     '媒体管理', 3, 'media:upload',            10),
    ('媒体详情',     '媒体管理', 3, 'media:detail',            11),
    ('更新媒体',     '媒体管理', 3, 'media:update',            12),
    ('删除媒体',     '媒体管理', 3, 'media:delete',            13),
    ('媒体分类树',   '媒体管理', 3, 'media:category_tree',     14),
    ('新建分类',     '媒体管理', 3, 'media:category_create',   15),
    ('更新分类',     '媒体管理', 3, 'media:category_update',   16),
    ('删除分类',     '媒体管理', 3, 'media:category_delete',   17)
) AS v(title, parent_title, type, code, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.code AND x.deleted_time IS NULL);
