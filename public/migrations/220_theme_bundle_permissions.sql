-- ========================================
-- 220 · 主题包（Theme Bundle）导入导出权限点与超管策略（审计 VIS-014）
--
-- 背景：主题此前只是 themes 表的一行 JSONB —— 没有任何打包/导出/导入格式，
-- 第三方拿不到可交付的「设计令牌 + 页眉页脚块 + 若干页面 + 槽位预设」。
-- 本批补上主题包（zip：manifest + tokens + blocks + 可选 pages + 可选 slots + 预留预览图）
-- 与两个出口：GET /api/theme/export、POST /api/theme/import。
--
-- 为什么权限点必须与路由同批：两个接口挂在 authorizedAPI 组下（经 project 模块的
-- /api/theme 组），该组统一挂 CasbinMiddleware()，按**实际请求路径** enforce ——
-- 权限点缺失时没有任何策略能匹配，**含超管在内全员 403**
--（072/077/078/079/151/213/218 各踩过一次）。超管策略同理：只补权限点不补策略，
-- 超管自己也点不动。
--
-- 2 个权限点与 2 条路由一一对应（sys_permission 按 (api_path, api_method) 唯一）：
--   GET  /api/theme/export  导出主题包（zip 流）
--   POST /api/theme/import  导入主题包（multipart 上传）
--
-- 幂等：两段各自带 NOT EXISTS 守卫（重复执行不报错、不产生重复行）。
-- 注册：public/migrations/register_admin_i18n.go（Seed 220-theme-bundle-permissions）。
-- ========================================

-- 1) 权限点（2 条）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('project:theme_export', '导出主题包', 'project', '/api/theme/export', 'GET'),
    ('project:theme_import', '导入主题包', 'project', '/api/theme/import', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);

-- 2) 超管策略（身份表全量超管 × 这两个权限点，缺哪条补哪条）
--    判据用 LIKE 'project:theme_%' 把 035 已有的 6 个主题权限点一并覆盖：
--    语句本身是「缺哪条补哪条」，覆盖既有项不会产生任何写入。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code LIKE 'project:theme_%'
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
