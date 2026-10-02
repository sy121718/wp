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
-- 注册：public/migrations/register_core.go（Seed **999**-superadmin-all-policies）。
--
-- ⚠️ 文件名是 051，注册版本却是 **999** —— 这是**有意**的，不是笔误：
-- 本 seed 做的是「超管 = 全部启用权限点」的全量补全，必须排在**所有权限点 seed 之后**。
-- 用 051 前缀时它先于 072 等后来新增的权限点 seed 运行，检查时「没有缺失」→ 直接跳过，
-- 之后新插入的权限点永远补不上（实测后果见上）。挪回 051 会让这条静默失效。参见
-- register_core.go 里该 Seed 之上的完整论证。
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
