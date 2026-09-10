-- ========================================
-- go_wp — 补 media 换图 / 引用来源两个权限点
--
-- 背景（全项目审查发现，已验证）：
--   媒体路由 POST /api/media/replace 与 GET /api/media/references 早已注册并挂 Casbin，
--   但两个权限点从未 seed（030 只覆盖 list/detail/upload/update/delete/category*，
--   048 只覆盖 download/download_batch/variants_generate）。
--   Casbin 在无策略匹配时 Enforce 返回 false → 403，因此这两个接口对
--   **包括超管在内的全体用户**都是死链（功能完全不可用）。
--
-- 超管策略：本迁移只插权限点；超管 p 策略由 seed 051 的 CROSS JOIN 全量补全
--   （051 的 ConditionSQL 已同步修正为「仍有缺失才执行」，见 register.go）。
--
-- 幂等：每条 INSERT 带 NOT EXISTS 守卫，重复执行不报错、不重复插入。
-- 注册：public/migrations/register.go（Seed 072-media-replace-permissions）。
-- ========================================
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'media', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('media:replace',    '媒体换图',      '/api/media/replace',    'POST'),
    ('media:references', '媒体引用来源',  '/api/media/references', 'GET')
) AS v(code, name, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);
