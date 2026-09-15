-- 123_user.sql
-- 访客账号模块（issue #36）：用户侧身份的**结构化**落地。
--
-- 参考 WordPress 的信息模型但换掉它的实现方式：WP 把除 wp_users 十个列以外的所有东西
-- （姓名/昵称/简介/语言/后台配色/富文本开关/会话 token/序列化的角色数组/插件数据）
-- 全塞进 wp_usermeta 的 key-value —— 无 schema、无类型、无法索引，连角色都是
-- a:1:{s:13:"administrator";b:1;} 这样的 PHP 序列化串。
--
-- 这里的原则：**能用列表达的就不进 key-value**，也不建「预留给将来」的空表 ——
-- 开发阶段用不上的表就是负债（迁移 203 删掉的那几张：会话台账、应用密码、
-- 插件 KV、OAuth 绑定）。真正需要时再加一条迁移，成本与当初一样。
--
-- 结构范式照本仓既有的 sys_admin：登录安全计数 / 锁定时间 / 来源 IP 与归属地 /
-- 最后登录信息都是结构化列，扩展位用 JSONB。
--
-- 本迁移只建三张表：users（身份与认证）、user_profiles（资料）、user_preferences（前台偏好）。
-- 访客会话与登录设备台账**不落库**，全部在 Redis（见 internal/module/user/service/user_session_store.go）。

-- 1. users —— 身份与认证
CREATE TABLE IF NOT EXISTS users (
    id                    BIGSERIAL    PRIMARY KEY,
    username              VARCHAR(60)  NOT NULL,
    -- 空串表示「这个账号没有本站密码」（外部导入 / 后台代开的账号）：
    -- 登录逻辑据此拒绝密码登录，而不是让空串能匹配上任何哈希。
    password              VARCHAR(100) NOT NULL DEFAULT '',
    -- 邮箱可空：外部导入的账号可能没有邮箱。
    -- 空串（而非 NULL）表示「未提供」，配合下面的部分唯一索引：空串可重复，真实邮箱仍唯一。
    email                 VARCHAR(100) NOT NULL DEFAULT '',
    email_verified_at     TIMESTAMP(3),
    -- 1 正常 / 0 禁用 / 2 待激活（等待邮箱验证）；与 WP 的 user_status 同义但取值有定义
    status                SMALLINT     NOT NULL DEFAULT 1,
    -- nickname 是站内称呼、display_name 是公开展示名（WP 两者分离，保留这个区分）
    nickname              VARCHAR(60),
    display_name          VARCHAR(250),
    avatar                VARCHAR(255),
    -- 激活 / 重置密码的一次性凭据（WP 的 user_activation_key 不带过期，这里补上）
    activation_key        VARCHAR(64),
    activation_expires_at TIMESTAMP(3),
    -- 登录安全：与 sys_admin 同一套口径（失败计数用原子 SQL 递增，锁定期自动过期）
    login_failure_count   INTEGER      NOT NULL DEFAULT 0,
    locked_until_time     TIMESTAMP(3),
    last_failure_time     TIMESTAMP(3),
    -- 来源追溯：注册与最后登录的 IP + 归属地
    register_ip           VARCHAR(50),
    register_location     VARCHAR(100),
    last_login_ip         VARCHAR(50),
    last_login_location   VARCHAR(100),
    registered_at         TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_time       TIMESTAMP(3),
    last_active_at        TIMESTAMP(3),
    -- 扩展位：真非结构化的东西进 JSONB，不再另开一张 key-value 表
    metadata              JSONB,
    create_by             BIGINT       NOT NULL DEFAULT 0,
    create_time           TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time           TIMESTAMP(3),
    -- 注销 = **软删除**：行保留、数据不删，只标记注销时间。
    --
    -- 与「删除」的区别写在这里以免将来被改成物理删除：注销是可追溯的业务事实
    -- （历史内容、订单、审计要指向这个人），物理删除会让所有引用变成孤儿。
    --
    -- 连带取舍：用户名与邮箱**永久占用** —— 别人不能注册「刚刚注销的那个名字」，
    -- 否则会出现冒充（外人顶着原用户名与历史内容混淆）。若将来要允许复用，
    -- 把下面两个唯一索引改成部分唯一索引 WHERE deleted_at IS NULL 即可。
    deleted_at            TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at);
-- 登录名与邮箱**大小写不敏感唯一**（登录时不该因为大小写差异变成两个账号）
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (lower(username));
-- 邮箱唯一但**排除空串**：多个第三方账号可以都没有邮箱，真实邮箱仍不许重复。
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (lower(email)) WHERE email <> '';
CREATE INDEX IF NOT EXISTS idx_users_status ON users (status);
CREATE INDEX IF NOT EXISTS idx_users_activation_key ON users (activation_key) WHERE activation_key IS NOT NULL;

-- 2. user_profiles —— 资料（一对一）
CREATE TABLE IF NOT EXISTS user_profiles (
    id          BIGSERIAL   PRIMARY KEY,
    user_id     BIGINT      NOT NULL,
    first_name  VARCHAR(60),
    last_name   VARCHAR(60),
    gender      SMALLINT    NOT NULL DEFAULT 0,
    birthday    DATE,
    -- WP 的 description：个人简介
    bio         TEXT,
    -- WP 的 user_url
    website     VARCHAR(255),
    locale      VARCHAR(16),
    timezone    VARCHAR(64),
    country     VARCHAR(64),
    province    VARCHAR(64),
    city        VARCHAR(64),
    address     VARCHAR(255),
    postcode    VARCHAR(20),
    phone       VARCHAR(20),
    company     VARCHAR(100),
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_profiles_user_id ON user_profiles (user_id);

-- 3. user_preferences —— **前台**偏好（一对一）
--
-- 与 WP 的差异值得记一笔：WP 的 admin_color / rich_editing / show_admin_bar_front 是
-- **后台**偏好（给有 wp-admin 权限的人用）；访客账号没有后台，要的是前台体验与通知偏好。
CREATE TABLE IF NOT EXISTS user_preferences (
    id                 BIGSERIAL   PRIMARY KEY,
    user_id            BIGINT      NOT NULL,
    theme              VARCHAR(64),
    locale             VARCHAR(16),
    timezone           VARCHAR(64),
    page_size          SMALLINT    NOT NULL DEFAULT 20,
    email_notify       BOOLEAN     NOT NULL DEFAULT TRUE,
    sms_notify         BOOLEAN     NOT NULL DEFAULT FALSE,
    -- public / members / private：个人主页可见性
    profile_visibility VARCHAR(16) NOT NULL DEFAULT 'public',
    show_online        BOOLEAN     NOT NULL DEFAULT TRUE,
    create_time        TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time        TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_preferences_user_id ON user_preferences (user_id);





