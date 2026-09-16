-- ========================================
-- 214 · 补齐「有路由、无权限点」的接口（5 条）
--
-- 背景：authorizedAPI 组挂了 CasbinMiddleware()，按**实际请求路径** enforce。
-- 权限点缺失 → 没有任何策略能匹配 → **含超管在内全员 403**
-- （072/077/078/079/151 各踩过一次，151 的注释里写得很清楚）。
--
-- 这 5 条是 bash scripts/check-permission-gaps.sh 审计出来的 ——
-- 不是翻代码猜的，是把**运行时装配的路由表**与 sys_permission 做差集：
--
--   GET  /api/build/jobs              构建任务列表（构建队列可见性）
--   GET  /api/build/queue             队列状态（运维视图）
--   POST /api/build/retry             重试失败的构建
--   POST /api/publication/seo-audit   SEO 巡检（pubhttp，产物链接/元数据核对）
--   GET  /api/order/coupon/count-audit 券计数对账（DB-021，只读巡检）
--
-- 五条都是后台运维/审计视图，**不在**「必须匿名可达」或「登录后人人可用」的豁免清单里：
-- 构建队列与 SEO 巡检暴露全站结构与失败原因，券计数对账暴露核销数据 ——
-- 都该按权限点收口，而不是登录即可读。
--
-- 顺带把「有没有漏」常态化：这一批是脚本先于 CI 发现的，说明审计没进流水线。
-- 修完请跑 bash scripts/check-permission-gaps.sh 确认归零。
--
-- 超管策略与权限点写在同一支 seed（151 的做法）：只补权限点不会让超管能访问，
-- 策略表要另有一行；分两个迁移写会出现「接口上线但超管也点不动」的空窗。
--
-- 注册：public/migrations/register_analytics.go（Seed 214-missing-permission-points-2）。
-- ========================================

-- 1) 权限点（5 条）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('build:jobs',               '构建任务列表', 'build',       '/api/build/jobs',                 'GET'),
    ('build:queue',              '构建队列状态', 'build',       '/api/build/queue',                'GET'),
    ('build:retry',              '重试构建',     'build',       '/api/build/retry',                'POST'),
    ('publication:seo_audit',    'SEO 审计',     'publication', '/api/publication/seo-audit',      'POST'),
    ('order:coupon_count_audit', '券计数对账',   'order',       '/api/order/coupon/count-audit',   'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);

-- 2) 超管策略（身份表全量超管 × 这 5 个权限点，缺哪条补哪条）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code IN ('build:jobs', 'build:queue', 'build:retry', 'publication:seo_audit', 'order:coupon_count_audit')
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
