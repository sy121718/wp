-- ========================================
-- go_wp — 超管全量策略补全（从 sys_permission 全表生成）
--
-- 背景：030/031/032/033/035 等 seed 各自硬编码了「权限点 + 超管策略」列表，
-- 但 registerSeed 的 ConditionSQL 语义是「权限点已存在则跳过整个 seed」——
-- 一旦权限点先落库（例如由别的迁移或手工插入），对应策略永远不会补。
-- 实测后果：超管缺少 plugin / theme / content / navigation 等模块的 p 策略，
-- 后台点「插件安装」「主题激活」等全部 403（无权限访问）。
--
-- 本迁移把「超管 = 全部启用权限点」固化为一条 CROSS JOIN 语句：
-- 权限点表新增条目后重新执行本 seed 即可补全，不再需要逐个模块维护策略清单。
--
-- 幂等：NOT EXISTS 守卫；重复执行只补缺失项。
-- 注册：public/migrations/register.go（Seed 051-superadmin-all-policies）。
-- ========================================
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.api_path <> ''
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
