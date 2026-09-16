-- ========================================
-- 213 · webhook 模块权限点与超管策略（OSS-006 接线）
--
-- 背景：webhook 的外部集成通道（端点白名单 + 投递日志 + worker）在 199 就建好了，
-- 但**从未接线** —— 没有 contract / inbound / 权限点，service 没有任何调用方，
-- 是一段实现完整却不参与运行的死代码。本次把它接上，权限点在这里补。
--
-- authorizedAPI 组挂了 CasbinMiddleware()，按**实际请求路径** enforce：
-- 权限点缺失 → 没有任何策略能匹配 → **含超管在内全员 403**
-- （072/077/078/079/151 各踩过一次）。新增挂在该组下的接口必须同批 seed。
--
-- 超管策略与权限点写在同一个 seed 里（151 的做法）：只补权限点不会让超管能访问，
-- 策略表要另有一行；分两个迁移写会出现「接口上线但超管也点不动」的空窗。
-- 整条 seed 的跳过条件要求 6 个权限点全部在，两段各自另有 NOT EXISTS 守卫（幂等）。
--
-- 注册：public/migrations/register_analytics.go（Seed 213-webhook-permissions）。
-- ========================================

-- 1) 权限点（6 条，module 与 api_path/api_method 一一对应业务路由，无 RESTful 路径参数）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('webhook:endpoint_list',   '集成端点列表', 'webhook', '/api/webhook/endpoint/list',   'GET'),
    ('webhook:endpoint_save',   '保存集成端点', 'webhook', '/api/webhook/endpoint/save',   'POST'),
    ('webhook:endpoint_delete', '删除集成端点', 'webhook', '/api/webhook/endpoint/delete', 'POST'),
    ('webhook:endpoint_status', '启停集成端点', 'webhook', '/api/webhook/endpoint/status', 'POST'),
    ('webhook:delivery_list',   '投递日志',     'webhook', '/api/webhook/delivery/list',   'GET'),
    ('webhook:delivery_retry',  '重投投递',     'webhook', '/api/webhook/delivery/retry',  'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);

-- 2) 超管策略（身份表全量超管 × 这 6 个权限点，缺哪条补哪条）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code LIKE 'webhook:%'
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
