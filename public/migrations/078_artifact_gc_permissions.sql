-- ========================================
-- go_wp — 产物回收接口权限点
--
-- 背景：新增 POST /api/page/artifact/gc。接口挂 Casbin，权限点不 seed 则 Enforce
-- 无策略匹配 → 含超管在内全员 403（与 072/077 同因）。
-- 超管策略由 seed 999-superadmin-all-policies 自动补全。
--
-- 注册：public/migrations/register.go（Seed 078-artifact-gc-permissions）。
-- ========================================
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'page:artifact_gc', '回收产物文件', 'page', '/api/page/artifact/gc', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'page:artifact_gc');
