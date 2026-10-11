-- 596 · 一次性数据迁移的执行标记表（sys_migration_marker）
-- ========================================
-- 本仓没有 migration/seed 台账表：注册项各自靠 CheckSQL 幂等，权限行之类「业务数据本身」
-- 又随时可能被运维删改，不能充当「这个一次性动作做过了」的判据（拿它当标记，运维删掉一行
-- 就会让一次性动作重放 —— 正是 597 要避免的缺陷形态）。所以一次性数据修正需要一张
-- **只由迁移写入、内容不可变**的标记表，这张表就是它。
--
-- 为什么单独一条结构迁移、且必须早于使用者（597）：
--   PostgreSQL 在解析/规划阶段就会解析整条 SQL 里的所有 FROM 关系，**CASE 的短路只在执行期**。
--   如果把「建表」与「查标记行」写进同一条迁移的 CheckSQL（`CASE WHEN to_regclass(...) IS NULL
--   THEN 0 WHEN EXISTS (SELECT 1 FROM sys_migration_marker ...) ...`），全新库里表还不存在时，
--   后一个分支里的静态子查询会在解析期直接报 `relation "sys_migration_marker" does not exist`
--   —— 迁移跑不起来，to_regclass 那道守卫形同虚设。
--   因此：先由本条迁移（无 CheckSQL，走默认的「表是否已建」检查）建表，使用者再去查行。
--
-- 表结构语义：
--   version      主键，与 public/migrations 注册项的 Version **逐字一致**；主键 ⇒ 不可重复、不可混淆；
--   component    归属主题（如 permission），便于按主题审计；
--   note         这次动作做了什么、为什么只做一次（写清判据，供日后审计）；
--   completed_at 完成时间（默认 now()，迁移内不覆盖）；
--   completed_by 执行者账号 id：迁移由启动过程执行，固定 0。
-- ========================================

CREATE TABLE IF NOT EXISTS sys_migration_marker (
    version      VARCHAR(120) PRIMARY KEY,
    component    VARCHAR(80)  NOT NULL,
    note         VARCHAR(500),
    completed_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    completed_by BIGINT       NOT NULL DEFAULT 0
);

COMMENT ON TABLE  sys_migration_marker IS '一次性数据迁移的执行台账：version 与注册项 Version 逐字一致，行存在即代表该动作已完成、永不重放';
COMMENT ON COLUMN sys_migration_marker.version IS '迁移版本号（主键，与 public/migrations 注册项 Version 一致）';
COMMENT ON COLUMN sys_migration_marker.component IS '归属主题（如 permission）';
COMMENT ON COLUMN sys_migration_marker.note IS '这次动作做了什么、为什么只做一次（判据写在这里，供日后审计）';
COMMENT ON COLUMN sys_migration_marker.completed_at IS '标记写入时间（= 一次性动作完成时间）';
COMMENT ON COLUMN sys_migration_marker.completed_by IS '执行者账号 id；迁移由启动过程执行，固定为 0';

-- 回滚（手工）：DROP TABLE IF EXISTS sys_migration_marker;
-- 注意：删表会让所有一次性动作失去「已执行」的判据 —— 删之前请确认它们重跑是安全的。
