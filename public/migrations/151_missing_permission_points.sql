-- ========================================
-- 151 · 补齐「有路由、无权限点」的接口（2 条）
--
-- 背景：authorizedAPI 组挂了 CasbinMiddleware()，它按**实际请求路径** enforce。
-- 权限点表里没有对应条目 → 没有任何策略能匹配 → **含超管在内全员 403**
-- （与 072/077/078/079 同因，那几个是新增接口时顺手 seed 掉的）。
--
-- 本次缺口不是靠翻代码猜的，是把**运行时路由表**与 sys_permission 做差集得出的
-- （257 条 /api 路由 vs 252 条权限点，差 7 条）：
--
--   POST /api/page/delete   删页面 —— 所以后台一直点不动删除，只能走 SQL
--   POST /api/block/clone   区块克隆 —— workbench 的块「插入-复制」走的接口
--
-- 另外 5 条（admin login/logout/profile/routes 与 captcha）**有意豁免**，不在此补：
-- 它们要么必须匿名可达（登录、验证码），要么是登录后人人可用的自用接口。
--
-- 超管策略：只补权限点并不会让超管能访问 —— 策略表要另有一行
-- （sys_casbin_rule 的 p 策略：v0=管理员 id / v1=路径 / v2=方法 / v3=权限码）。
-- 079 那条把策略交给 999 全量 seed 事后补，中间有空窗；这里在同一个迁移里写完，
-- SQL 文件内两段都是幂等的（NOT EXISTS 守卫），整条 seed 的跳过条件要求两段都已生效。
--
-- 注册：public/migrations/register.go（Seed 151-missing-permission-points）。
-- ========================================

-- 1) 权限点
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'page:delete', '删除页面', 'page', '/api/page/delete', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'page:delete');

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'block:clone', '克隆区块', 'block', '/api/block/clone', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'block:clone');

-- 2) 超管策略（身份表全量超管 × 这两个权限点，缺哪条补哪条）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code IN ('page:delete', 'block:clone')
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
