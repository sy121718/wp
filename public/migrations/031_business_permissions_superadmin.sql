-- ========================================
-- go_wp — 默认超管（is_admin=1）业务权限全量策略（sys_casbin_rule）
--
-- 与 030_business_permissions.sql 的权限点一一对应，以 p, user_id, path, method, code
-- 形式直接授权给 sys_admin.is_admin=1 的管理员：
--   - 迁移后超管无需手工配置即可访问全部业务 API（与 admin 领域「策略驱动」一致）；
--   - 非超管不在此授策略，默认拒绝，需经角色/用户授权接口（RoleMenuSave / AdminMenuSave）
--     按 permission_code 分配后放行。
--
-- 注意：p, user_id 策略会被管理端的「全量替换用户直接权限」操作覆盖（现有体系行为，
-- 与 admin 领域一致）；如需重建可再次执行本 seed（幂等）。
-- 注册：public/migrations/register.go（Seed 031-business-permissions-superadmin）。
-- ========================================
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/page/list',                 'GET',  'page:list'),
    ('/api/page/detail',               'GET',  'page:detail'),
    ('/api/page/create',               'POST', 'page:create'),
    ('/api/page/draft/save',           'POST', 'page:draft_save'),
    ('/api/page/revision/list',        'GET',  'page:revision_list'),
    ('/api/page/build',                'POST', 'page:build'),
    ('/api/page/publish',              'POST', 'page:publish'),
    ('/api/page/rollback',             'POST', 'page:rollback'),
    ('/api/page/url/update',           'POST', 'page:url_update'),
    ('/api/project/list',              'GET',  'project:list'),
    ('/api/project/detail',            'GET',  'project:detail'),
    ('/api/project/create',            'POST', 'project:create'),
    ('/api/project/update',            'POST', 'project:update'),
    ('/api/block/list',                'GET',  'block:list'),
    ('/api/block/detail',              'GET',  'block:detail'),
    ('/api/block/create',              'POST', 'block:create'),
    ('/api/block/update',              'POST', 'block:update'),
    ('/api/block/delete',              'POST', 'block:delete'),
    ('/api/media/list',                'GET',  'media:list'),
    ('/api/media/detail',              'GET',  'media:detail'),
    ('/api/media/upload',              'POST', 'media:upload'),
    ('/api/media/update',              'POST', 'media:update'),
    ('/api/media/delete',              'POST', 'media:delete'),
    ('/api/media/category/tree',       'GET',  'media:category_tree'),
    ('/api/media/category/create',     'POST', 'media:category_create'),
    ('/api/media/category/update',     'POST', 'media:category_update'),
    ('/api/media/category/delete',     'POST', 'media:category_delete'),
    ('/api/artifact/detail',           'GET',  'artifact:detail'),
    ('/api/publication/receipts/pending', 'GET', 'publication:receipts_pending')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = t.path AND r.v2 = t.method AND r.v3 = t.code
  );
