-- 199_rls_project_locales.sql — project_locales 启用行级安全（RLS）做工程隔离（DB-009 第一批试点）。
--
-- 背景：多工程数据此前只靠应用层 Where("project_id = ?") 隔离，任何一处漏加条件
-- 都会跨工程读/写数据。本迁移为 project_locales 启用 PostgreSQL 行级安全，
-- 由数据库兜底强制隔离（DB-009，P7 medium）。
--
-- 设计：
--   * 会话/事务变量 app.project_id 由 Go 侧在事务内以 set_config(..., is_local => true)
--     设置（见 projectmodel.withProjectScope），事务结束自动还原，连接池复用安全。
--   * 策略 USING 与 WITH CHECK 同时约束读与写；current_setting 第二参 true 表示
--     未设置时返回 NULL 而不是报错 —— 变量缺失时谓词为 NULL，行不可见（fail closed），
--     不允许「忘记设置变量就退化为全表可见」。
--   * FORCE ROW LEVEL SECURITY：即使是表属主（应用连接用户 root）也受策略约束，
--     防止属主身份绕过隔离。
--   * project_locales 无全局行（project_id 是主键一部分、NOT NULL），不需要
--     「project_id IS NULL 对所有工程可见」的分支；该分支留给后续批次的全局表。
--
-- 幂等性：迁移以 CheckSQL 按 pg_policies 中策略名判定是否已应用；
-- ENABLE/FORCE 重复执行安全，CREATE POLICY 无 IF NOT EXISTS，由 CheckSQL 兜住。

ALTER TABLE project_locales ENABLE ROW LEVEL SECURITY;

ALTER TABLE project_locales FORCE ROW LEVEL SECURITY;

CREATE POLICY project_isolation ON project_locales
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);
