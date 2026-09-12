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
    -- 第三方登录注册的账号**没有密码**：空串表示「只能走第三方登录」，
    -- 登录逻辑据此拒绝密码登录（而不是让空串能匹配上任何哈希）。
    password              VARCHAR(100) NOT NULL DEFAULT '',
    -- 邮箱可空：微信 / QQ 默认**不返回邮箱**（需额外申请权限），第三方注册的账号可能没有。
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
    -- 扩展位：真非结构化的东西进 JSONB，不进 user_meta（那是留给插件的）
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

-- 4. user_sessions —— 登录设备台账（WP 的 session_tokens 用途）
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

-- 5. user_app_passwords —— 应用密码（WP 的 _application_passwords，给 API 访问）
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

-- 6. user_meta —— **只给插件**的 key-value（学 WP 的灵活性，但划死边界）
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

-- 7. user_oauth_bindings —— 第三方登录绑定（**预留给后续扩展**：Google / QQ / 微信 / GitHub…）
--
-- 现在就建表的理由：这类「身份绑定」的落点一旦定错，后补要动 users 表与登录逻辑；
-- 表放在这里不影响任何现有代码，接 provider 时直接用。
--
-- 设计要点：
--   · 唯一键是 (provider, open_id)：同一平台的同一账号只能绑一个用户；
--   · 微信特殊：openid 是「应用内」标识、unionid 是「同一开放平台下跨应用」标识，
--     两个都要存 —— 只存 openid 会导致同一用户在不同应用里被认成两个人；
--   · nickname / avatar 是**第三方返回的快照**，随时可能变，只作展示与回填参考，
--     不作为账号的权威字段（权威字段始终在 users 上）；
--   · raw 存原始返回，排障与将来适配用。
--
-- 归属：本表只管**访客账号**的绑定。后台管理员的第三方登录走 admin 模块自己的绑定表 ——
-- 不共用一张表（AGENTS.md 表隔离约定）；共用的只有 pkg/oauth 里的协议实现。
CREATE TABLE IF NOT EXISTS user_oauth_bindings (
    id            BIGSERIAL    PRIMARY KEY,
    user_id       BIGINT       NOT NULL,
    provider      VARCHAR(32)  NOT NULL,
    open_id       VARCHAR(191) NOT NULL,
    union_id      VARCHAR(191),
    nickname      VARCHAR(191),
    avatar        VARCHAR(500),
    raw           JSONB,
    bound_at      TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP(3)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_oauth_provider_openid ON user_oauth_bindings (provider, open_id);
CREATE INDEX IF NOT EXISTS idx_user_oauth_user_id ON user_oauth_bindings (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_oauth_unionid ON user_oauth_bindings (provider, union_id) WHERE union_id IS NOT NULL;
