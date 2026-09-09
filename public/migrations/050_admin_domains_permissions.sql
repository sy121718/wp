-- ========================================
-- go_wp — 管理面六领域权限点与超管策略（admin / role / permission / menu / dept / datarule）
--
-- 背景：六领域 API 自合并进 admin 大模块起即挂 CasbinMiddleware，但权限点从未 seed
-- （030 仅覆盖 page/project/block/media/artifact/publication），导致超管访问
-- /api/role/* 等也被拒（403 无权限访问），dashboard 六领域页面的写操作全部不可用。
--
-- 内容：
--   1) sys_permission：六领域 34 个权限点（与 admin_router.go 路由一一对应）
--   2) sys_casbin_rule：超管（is_admin=1）全量 p 策略（对齐 031 模式）
--
-- 幂等：NOT EXISTS 守卫；如需重建可重复执行。
-- 注册：public/migrations/register.go（Seed 047-admin-domains-permissions）。
-- ========================================

-- 1. 权限点
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('admin:list',            '管理员列表',        'admin',      '/api/admin/list',        'GET'),
    ('admin:detail',          '管理员详情',        'admin',      '/api/admin/detail',      'GET'),
    ('admin:create',          '新建管理员',        'admin',      '/api/admin/create',      'POST'),
    ('admin:edit',            '编辑管理员',        'admin',      '/api/admin/edit',        'POST'),
    ('admin:delete',          '删除管理员',        'admin',      '/api/admin/delete',      'POST'),
    ('admin:role_list',       '管理员角色查看',    'admin',      '/api/admin/role/list',   'GET'),
    ('admin:role_save',       '管理员角色分配',    'admin',      '/api/admin/role/save',   'POST'),
    ('admin:menu_list',       '管理员菜单查看',    'admin',      '/api/admin/menu/list',   'GET'),
    ('admin:menu_save',       '管理员菜单分配',    'admin',      '/api/admin/menu/save',   'POST'),
    ('role:list',             '角色列表',          'role',       '/api/role/list',         'GET'),
    ('role:detail',           '角色详情',          'role',       '/api/role/detail',       'GET'),
    ('role:create',           '新建角色',          'role',       '/api/role/create',       'POST'),
    ('role:update',           '更新角色',          'role',       '/api/role/update',       'POST'),
    ('role:delete',           '删除角色',          'role',       '/api/role/delete',       'POST'),
    ('role:menu_list',        '角色菜单查看',      'role',       '/api/role/menu/list',    'GET'),
    ('role:menu_save',        '角色菜单分配',      'role',       '/api/role/menu/save',    'POST'),
    ('role:user_list',        '角色用户查看',      'role',       '/api/role/user/list',    'GET'),
    ('role:user_save',        '角色用户分配',      'role',       '/api/role/user/save',    'POST'),
    ('permission:list',       '权限点列表',        'permission', '/api/permission/list',   'GET'),
    ('permission:detail',     '权限点详情',        'permission', '/api/permission/detail', 'GET'),
    ('permission:options',    '权限点选项',        'permission', '/api/permission/options','GET'),
    ('permission:create',     '新建权限点',        'permission', '/api/permission/create', 'POST'),
    ('permission:update',     '更新权限点',        'permission', '/api/permission/update', 'POST'),
    ('permission:delete',     '删除权限点',        'permission', '/api/permission/delete', 'POST'),
    ('menu:list',             '菜单树',            'menu',       '/api/menu/tree',         'GET'),
    ('menu:detail',           '菜单详情',          'menu',       '/api/menu/detail',       'GET'),
    ('menu:create',           '新建菜单',          'menu',       '/api/menu/create',       'POST'),
    ('menu:update',           '更新菜单',          'menu',       '/api/menu/update',       'POST'),
    ('menu:delete',           '删除菜单',          'menu',       '/api/menu/delete',       'POST'),
    ('dept:list',             '部门树',            'dept',       '/api/dept/tree',         'GET'),
    ('dept:detail',           '部门详情',          'dept',       '/api/dept/detail',       'GET'),
    ('dept:create',           '新建部门',          'dept',       '/api/dept/create',       'POST'),
    ('dept:update',           '更新部门',          'dept',       '/api/dept/update',       'POST'),
    ('dept:delete',           '删除部门',          'dept',       '/api/dept/delete',       'POST'),
    ('dept:user_list',        '部门用户查看',      'dept',       '/api/dept/user/list',    'GET'),
    ('dept:user_save',        '部门用户分配',      'dept',       '/api/dept/user/save',    'POST'),
    ('datarule:list',         '数据规则列表',      'datarule',   '/api/datarule/list',     'GET'),
    ('datarule:detail',       '数据规则详情',      'datarule',   '/api/datarule/detail',   'GET'),
    ('datarule:create',       '新建数据规则',      'datarule',   '/api/datarule/create',   'POST'),
    ('datarule:update',       '更新数据规则',      'datarule',   '/api/datarule/update',   'POST'),
    ('datarule:delete',       '删除数据规则',      'datarule',   '/api/datarule/delete',   'POST'),
    ('datarule:schema_list',  '数据域清单',        'datarule',   '/api/datarule/schema/list', 'GET'),
    ('datarule:schema_detail','数据域详情',        'datarule',   '/api/datarule/schema/detail','GET'),
    ('datarule:assignment_list',   '规则分配查看', 'datarule',   '/api/datarule/assignment/list', 'GET'),
    ('datarule:assignment_save',   '规则分配保存', 'datarule',   '/api/datarule/assignment/save', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission x WHERE x.permission_code = v.code);

-- 2. 超管全量策略（对齐 031 模式：p, user_id, path, method, code）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/admin/list',        'GET',  'admin:list'),
    ('/api/admin/detail',      'GET',  'admin:detail'),
    ('/api/admin/create',      'POST', 'admin:create'),
    ('/api/admin/edit',        'POST', 'admin:edit'),
    ('/api/admin/delete',      'POST', 'admin:delete'),
    ('/api/admin/role/list',   'GET',  'admin:role_list'),
    ('/api/admin/role/save',   'POST', 'admin:role_save'),
    ('/api/admin/menu/list',   'GET',  'admin:menu_list'),
    ('/api/admin/menu/save',   'POST', 'admin:menu_save'),
    ('/api/role/list',         'GET',  'role:list'),
    ('/api/role/detail',       'GET',  'role:detail'),
    ('/api/role/create',       'POST', 'role:create'),
    ('/api/role/update',       'POST', 'role:update'),
    ('/api/role/delete',       'POST', 'role:delete'),
    ('/api/role/menu/list',    'GET',  'role:menu_list'),
    ('/api/role/menu/save',    'POST', 'role:menu_save'),
    ('/api/role/user/list',    'GET',  'role:user_list'),
    ('/api/role/user/save',    'POST', 'role:user_save'),
    ('/api/permission/list',   'GET',  'permission:list'),
    ('/api/permission/detail', 'GET',  'permission:detail'),
    ('/api/permission/options','GET',  'permission:options'),
    ('/api/permission/create', 'POST', 'permission:create'),
    ('/api/permission/update', 'POST', 'permission:update'),
    ('/api/permission/delete', 'POST', 'permission:delete'),
    ('/api/menu/tree',         'GET',  'menu:list'),
    ('/api/menu/detail',       'GET',  'menu:detail'),
    ('/api/menu/create',       'POST', 'menu:create'),
    ('/api/menu/update',       'POST', 'menu:update'),
    ('/api/menu/delete',       'POST', 'menu:delete'),
    ('/api/dept/tree',         'GET',  'dept:list'),
    ('/api/dept/detail',       'GET',  'dept:detail'),
    ('/api/dept/create',       'POST', 'dept:create'),
    ('/api/dept/update',       'POST', 'dept:update'),
    ('/api/dept/delete',       'POST', 'dept:delete'),
    ('/api/dept/user/list',    'GET',  'dept:user_list'),
    ('/api/dept/user/save',    'POST', 'dept:user_save'),
    ('/api/datarule/list',     'GET',  'datarule:list'),
    ('/api/datarule/detail',   'GET',  'datarule:detail'),
    ('/api/datarule/create',   'POST', 'datarule:create'),
    ('/api/datarule/update',   'POST', 'datarule:update'),
    ('/api/datarule/delete',   'POST', 'datarule:delete'),
    ('/api/datarule/schema/list',    'GET',  'datarule:schema_list'),
    ('/api/datarule/schema/detail',  'GET',  'datarule:schema_detail'),
    ('/api/datarule/assignment/list','GET',  'datarule:assignment_list'),
    ('/api/datarule/assignment/save','POST', 'datarule:assignment_save')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = t.path AND r.v2 = t.method AND r.v3 = t.code
  );
