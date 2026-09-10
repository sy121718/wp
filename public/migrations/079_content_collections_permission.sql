-- ========================================
-- go_wp — 内容集合元数据接口权限点
--
-- 背景：新增 GET /api/content/collections。接口挂 Casbin，权限点不 seed 则
-- Enforce 无策略匹配 → 含超管在内全员 403（与 072/077/078 同因）。
-- 该接口供工作台渲染「集合字段下拉」与内置组件（cardstack）字段校验使用。
-- 超管策略由 seed 999-superadmin-all-policies 自动补全。
--
-- 注册：public/migrations/register.go（Seed 079-content-collections-permission）。
-- ========================================
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'content:collections', '查看内容集合元数据', 'content', '/api/content/collections', 'GET', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'content:collections');
