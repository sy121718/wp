-- ========================================
-- go_wp — 灾难恢复接口权限点（产物重建 + 激活面巡检）
--
-- 背景：新增 POST /api/page/artifact/rebuild 与 GET /api/page/publication/audit。
-- 接口挂了 Casbin，权限点不 seed 则 Enforce 无策略匹配 → 含超管在内全员 403
--（media:replace 死链即此因，见 072）。超管策略由 seed 999-superadmin-all-policies
-- 的 CROSS JOIN 自动补全。
--
-- 幂等：NOT EXISTS 守卫。
-- 注册：public/migrations/register.go（Seed 077-recovery-permissions）。
-- ========================================
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, 'page', v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('page:artifact_rebuild',  '重建产物文件', '/api/page/artifact/rebuild',  'POST'),
    ('page:publication_audit', '发布面巡检',   '/api/page/publication/audit', 'GET')
) AS v(code, name, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);
