-- ========================================
-- 152 · 后台客户管理权限点（4 条）+ 超管策略
--
-- 背景：/api/customer/{list,get,status,unlock} 挂在 authorizedAPI 组上，该组统一用
-- CasbinMiddleware() 按**实际请求路径** enforce。权限点表里没有对应条目时，
-- 没有任何策略能匹配 → **含超管在内全员 403**（072/077/078/079/151 各踩过一次）。
--
-- 客户管理页（/admin/customers）的两个写按钮走的是同一批权限点的**路径形式**
-- （builtin.CasbinMiddlewareForPath("/api/customer/status" 与 "/api/customer/unlock")），
-- 因此页面与接口共用同一份授权，不需要给页面另立一套。
--
-- 超管策略必须与权限点**同批**写入：079 把策略交给 999 全量 seed 事后补，中间有空窗；
-- 151 起改为同一个迁移里写完。两段都带 NOT EXISTS 守卫，所以整条 seed 的跳过条件
-- 要求两段都已生效（只查权限点会让「权限点先落库、策略后补」的那一半永远补不上）。
--
-- 权限码用 user: 前缀（模块名，见 AGENTS.md 命名约束：user = 访问面访客账号），
-- 路径用 /api/customer/* —— 与公开面的 /user/* 只差一段前缀，翻日志时几乎分不出来，
-- 而这两个面的越权含义完全相反。
--
-- 注册：public/migrations/register.go（Seed 152-customer-admin-permissions）。
-- ========================================

-- 1) 权限点
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'user:customer_list', '查看客户列表', 'user', '/api/customer/list', 'GET', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'user:customer_list');

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'user:customer_detail', '查看客户详情', 'user', '/api/customer/get', 'GET', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'user:customer_detail');

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'user:customer_status', '停用/启用客户账号', 'user', '/api/customer/status', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'user:customer_status');

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'user:customer_unlock', '解除客户账号锁定', 'user', '/api/customer/unlock', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'user:customer_unlock');

-- 2) 超管策略（身份表里的全部超管 × 这 4 个权限点，缺哪条补哪条）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code IN ('user:customer_list', 'user:customer_detail', 'user:customer_status', 'user:customer_unlock')
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
