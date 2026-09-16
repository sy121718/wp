-- ========================================
-- go_wp — 默认超管账号（让全新部署能登录）
--
-- 为什么需要它：sys_admin 此前**没有任何创建路径** —— 既不在迁移里，也不在应用启动
-- 逻辑里（全仓 grep 不到 INSERT INTO sys_admin）。于是全新部署的库 sys_admin 是空的：
-- 没有人能登录，031 也没有授权对象（超管策略 0 条）。老库里那个 admin 是开发期手工插的，
-- 所以这个缺陷一直没暴露 —— 直到在**全新库**上跑 check-permission-gaps.sh。
--
-- 凭据：admin / A123456.!
--   · 这是**默认口令**：部署后请立刻改（管理端改密）。仓库里只存 bcrypt hash，
--     明文不落库也不入库；hash 的 cost=10 与登录校验（bcrypt.CompareHashAndPassword）一致。
--
-- 幂等：按 username 判定，已存在则整条不插 —— **不会覆盖已有账号的口令**
--   （老库的 admin 有自己改过的密码，本 seed 在那边必然跳过）。
-- 顺序：版本 030a 排在 030（权限点）之后、031（超管策略）之前 —— 031 从
--   sys_admin WHERE is_admin = 1 取授权对象，本 seed 必须先跑，否则超管拿不到策略。
-- ========================================
INSERT INTO sys_admin (username, password, name, status, is_admin, create_by, create_time, update_by, update_time)
SELECT 'admin',
       '$2a$10$nVJqJvlNvbZqF26X/un.huZEhsw8.Dyv0GvH/9XfFu6DhOfQ3YDc2',
       '超级管理员',
       1,
       1,
       0,
        NOW(),
       0,
        NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_admin WHERE username = 'admin');
