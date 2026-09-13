-- ========================================
-- 154 · 详情页改 URL 权限点（presentation:update_url）
--
-- 背景：presentation 实例此前只能「删实例再重建」才能换路径 —— 内核
-- （pipeline.Publisher.UpdateURL）与手工页面（page.Service.UpdateURL）都支持
-- 改 URL，唯独自动发布实例把 url_path 当成不可变身份列，发布后没有改路径的出口。
--
-- 本次新增 POST /api/presentation/update-url 承载改 URL：新路径构建激活后，
-- 旧路径按 WithRedirect 登记 301 或取消激活。authorizedAPI 组按**实际请求路径**
-- enforce，权限点与超管策略必须同批 seed，否则含超管在内全员 403
-- （与 151/152 同一原因：有路由、无权限点）。
--
-- 为什么不复用 presentation:create / rebuild：改 URL 会动线上路径与旧链接的
-- 未来行为（301 还是 404），与「发布」「重建」是不同影响面的动作；合并成一个
-- 权限点后就无法表达「允许发布、不允许改 URL」。
--
-- 注册：public/migrations/register.go（Seed 154-presentation-update-url-permission）。
-- ========================================

-- 1) 权限点
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'presentation:update_url', '详情页改 URL', 'presentation', '/api/presentation/update-url', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'presentation:update_url');

-- 2) 超管策略（身份表全量超管 × 这一个权限点，缺哪条补哪条）
--
-- 只补权限点并不会让超管能访问：策略表要另有一行（sys_casbin_rule 的 p 策略，
-- v0=管理员 id / v1=路径 / v2=方法 / v3=权限码）。两段写在同一个迁移里，
-- 避免 079 那种「权限点先落库、策略事后补」的空窗。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code = 'presentation:update_url'
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
