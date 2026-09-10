-- ========================================
-- go_wp — 导航项路径唯一补数据库层约束
--
-- 背景（全项目审查发现）：navigation 的路径唯一校验是 check-then-act ——
-- 先 ExistsPath 查重，再 Create。navigations 此前只有主键索引，并发写入
-- 同一 (project, kind, path) 时两次请求都能通过校验，各写一行，构建期
-- 导航树出现重复菜单项。
-- 注册：public/migrations/register.go（Migration 075-navigation-path-unique）。
-- ========================================
CREATE UNIQUE INDEX IF NOT EXISTS uq_navigation_project_kind_path
    ON navigations (project_id, kind, path);
