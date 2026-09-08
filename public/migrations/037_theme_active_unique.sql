-- 037_theme_active_unique.sql — themes 表「同工程单激活主题」部分唯一索引。
--
-- 背景：ActivateTheme 事务内「全置 false → 目标置 true」在 PG READ COMMITTED 隔离级别下
-- 存在并发竞态，两个并发请求可能各自读到对方的未提交前状态，最终产生同一工程两条
-- is_active=true 记录。本迁移加部分唯一索引兜底：同工程内 is_active=true 至多一行，
-- 并发激活 / 首建自动激活的冲突由数据库唯一约束拒绝（幂等）。
--
-- 说明：is_active 为 boolean 列（020_themes.sql 建表），可直接作为索引谓词；
-- CREATE UNIQUE INDEX IF NOT EXISTS 幂等，重复执行安全。
-- 若历史并发竞态已产生「同工程多条 is_active=true」的脏数据，本索引创建将报
-- duplicate key 错误以显式暴露问题，需先人工清理脏数据后再执行本迁移。

CREATE UNIQUE INDEX IF NOT EXISTS uq_themes_project_active
    ON themes(project_id)
    WHERE is_active = true;
