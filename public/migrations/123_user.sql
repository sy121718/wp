-- 123_user.sql
-- 访客账号模块（issue #36）：用户侧身份的**结构化**落地。
--
-- 参考 WordPress 的信息模型但换掉它的实现方式：WP 把除 wp_users 十个列以外的所有东西
-- （姓名/昵称/简介/语言/后台配色/富文本开关/会话 token/序列化的角色数组/插件数据）
-- 全塞进 wp_usermeta 的 key-value —— 无 schema、无类型、无法索引，连角色都是
-- a:1:{s:13:"administrator";b:1;} 这样的 PHP 序列化串。
--
-- 这里的原则：**能用列表达的就不许进 key-value**。user_meta 只留给插件扩展，
-- 核心功能一旦依赖它就会长出第二张 usermeta。
--
-- 结构范式照本仓既有的 sys_admin：登录安全计数 / 锁定时间 / 来源 IP 与归属地 /
-- 最后登录信息都是结构化列，扩展位用 JSONB。

-- 1. users —— 身份与认证
CREATE TABLE IF NOT EXISTS users (
    id                    BIGSERIAL    PRIMARY KEY,
    username              VARCHAR(60)  NOT NULL,
    password              VARCHAR(100) NOT NULL,
    email                 VARCHAR(100) NOT NULL,
    email_verified_at     TIMESTAMP(3),
    -- 1 正常 / 0 禁用 / 2 待激活（等待邮箱验证）；与 WP 的 user_status 同义但取值有定义
    status                SMALLINT     NOT NULL DEFAULT 1,
    role_code             VARCHAR(32)  NOT NULL DEFAULT 'member',
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
    -- 扩展位：真非结构化的东西进 JSONB，不进 user_meta（那是留给插件的）
    metadata              JSONB,
    create_by             BIGINT       NOT NULL DEFAULT 0,
    create_time           TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time           TIMESTAMP(3)
);
-- 登录名与邮箱**大小写不敏感唯一**（登录时不该因为大小写差异变成两个账号）
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (lower(username));
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (lower(email));
CREATE INDEX IF NOT EXISTS idx_users_status ON users (status);
CREATE INDEX IF NOT EXISTS idx_users_role_code ON users (role_code);
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

-- 4. user_roles —— 角色字典（替掉 WP 的序列化 capabilities）
--
-- 一期单角色（users.role_code 引用本表）。WP 的多能力数组是给「一个用户同时是作者+编辑」
-- 这类后台权限组合用的；访客站不需要，真需要时再升成关联表。
CREATE TABLE IF NOT EXISTS user_roles (
    id          BIGSERIAL   PRIMARY KEY,
    code        VARCHAR(32) NOT NULL,
    name        VARCHAR(64) NOT NULL,
    description VARCHAR(255),
    -- 注册时的默认角色（有且只有一个，由服务层守卫）
    is_default  BOOLEAN     NOT NULL DEFAULT FALSE,
    sort        INTEGER     NOT NULL DEFAULT 0,
    status      SMALLINT    NOT NULL DEFAULT 1,
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_roles_code ON user_roles (code);

-- 5. user_sessions —— 登录设备台账（WP 的 session_tokens 用途）
--
-- 会话状态仍在 Redis（与 admin 共用基础设施）；本表负责让用户**看见并踢掉**自己的其它设备，
-- 以及回答「这个账号最近在哪登录过」。
CREATE TABLE IF NOT EXISTS user_sessions (
    id             BIGSERIAL   PRIMARY KEY,
    user_id        BIGINT      NOT NULL,
    -- 只存会话令牌的哈希，不存明文（同密码的处理原则）
    token_hash     VARCHAR(64) NOT NULL,
    user_agent     VARCHAR(255),
    ip             VARCHAR(50),
    location       VARCHAR(100),
    last_active_at TIMESTAMP(3),
    created_at     TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at     TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_sessions_token_hash ON user_sessions (token_hash);

-- 6. user_app_passwords —— 应用密码（WP 的 _application_passwords，给 API 访问）
CREATE TABLE IF NOT EXISTS user_app_passwords (
    id             BIGSERIAL    PRIMARY KEY,
    user_id        BIGINT       NOT NULL,
    name           VARCHAR(64)  NOT NULL,
    password_hash  VARCHAR(100) NOT NULL,
    last_used_at   TIMESTAMP(3),
    last_used_ip   VARCHAR(50),
    revoked_at     TIMESTAMP(3),
    create_time    TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_user_app_passwords_user_id ON user_app_passwords (user_id);

-- 7. user_meta —— **只给插件**的 key-value（学 WP 的灵活性，但划死边界）
--
-- 死线：核心功能禁止依赖本表。能用列表达的就必须建列 ——
-- 否则这里会长成第二张 wp_usermeta，而那正是本模块要避开的写法。
CREATE TABLE IF NOT EXISTS user_meta (
    id         BIGSERIAL   PRIMARY KEY,
    user_id    BIGINT      NOT NULL,
    meta_key   VARCHAR(191) NOT NULL,
    meta_value JSONB,
    create_time TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_meta_key ON user_meta (user_id, meta_key);

-- 内置角色：member 为注册默认，vip 作为「角色可扩」的示范（一期不参与任何判定逻辑）
INSERT INTO user_roles (code, name, description, is_default, sort)
VALUES
    ('member', '普通会员', '注册后的默认角色', TRUE, 0),
    ('vip', 'VIP 会员', '预留：升级后享受的权益角色', FALSE, 10)
ON CONFLICT (code) DO NOTHING;
