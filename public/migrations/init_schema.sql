-- ========================================
-- go_wp — PostgreSQL 建表 DDL
-- 由 GORM model tag 翻译为 PostgreSQL DDL
-- ========================================

-- 1. 管理员表
CREATE TABLE IF NOT EXISTS sys_admin (
    id                  BIGSERIAL    PRIMARY KEY,
    dept_id             BIGINT       NOT NULL DEFAULT 0,
    username            VARCHAR(50)  NOT NULL,
    password            VARCHAR(100) NOT NULL,
    name                VARCHAR(50),
    avatar              VARCHAR(255),
    email               VARCHAR(100),
    phone               VARCHAR(20),
    status              SMALLINT     NOT NULL DEFAULT 1,
    is_admin            SMALLINT     NOT NULL DEFAULT 0,
    login_failure_count INTEGER      NOT NULL DEFAULT 0,
    locked_until_time   TIMESTAMP(3),
    metadata            JSONB,
    last_failure_time   TIMESTAMP(3),
    register_ip         VARCHAR(50),
    register_location   VARCHAR(100),
    last_login_ip       VARCHAR(50),
    last_login_location VARCHAR(100),
    last_login_isp      VARCHAR(50),
    last_login_time     TIMESTAMP(3),
    create_by           BIGINT       NOT NULL DEFAULT 0,
    create_time         TIMESTAMP(3),
    update_by           BIGINT       NOT NULL DEFAULT 0,
    update_time         TIMESTAMP(3),
    remark              VARCHAR(255),
    CONSTRAINT uk_sys_admin_username UNIQUE (username)
);
CREATE INDEX IF NOT EXISTS idx_sys_admin_email ON sys_admin(email);
CREATE INDEX IF NOT EXISTS idx_sys_admin_phone ON sys_admin(phone);
CREATE INDEX IF NOT EXISTS idx_sys_admin_status ON sys_admin(status);

COMMENT ON TABLE sys_admin IS '管理员表';
COMMENT ON COLUMN sys_admin.dept_id IS '所属部门ID';
COMMENT ON COLUMN sys_admin.username IS '登录账号';
COMMENT ON COLUMN sys_admin.password IS '加密密码';
COMMENT ON COLUMN sys_admin.status IS '状态：1启用 2禁用 3封禁';
COMMENT ON COLUMN sys_admin.is_admin IS '是否超级管理员：0否 1是';
COMMENT ON COLUMN sys_admin.login_failure_count IS '连续登录失败次数';
COMMENT ON COLUMN sys_admin.metadata IS '扩展元数据';

-- 2. 角色表
CREATE TABLE IF NOT EXISTS sys_role (
    id          BIGSERIAL    PRIMARY KEY,
    role_code   VARCHAR(50)  NOT NULL,
    role_name   VARCHAR(100) NOT NULL,
    status      SMALLINT     NOT NULL DEFAULT 1,
    is_system   SMALLINT     NOT NULL DEFAULT 0,
    sort_order  INTEGER      NOT NULL DEFAULT 0,
    remark      VARCHAR(200),
    create_by   BIGINT       NOT NULL DEFAULT 0,
    create_time TIMESTAMP(3),
    update_by   BIGINT       NOT NULL DEFAULT 0,
    update_time TIMESTAMP(3),
    CONSTRAINT uk_sys_role_code UNIQUE (role_code)
);

COMMENT ON TABLE sys_role IS '角色表';
COMMENT ON COLUMN sys_role.role_code IS '角色编码';
COMMENT ON COLUMN sys_role.role_name IS '角色名称';
COMMENT ON COLUMN sys_role.status IS '状态：0禁用 1启用';
COMMENT ON COLUMN sys_role.is_system IS '是否系统内置：0否 1是';

-- 3. 权限点表
CREATE TABLE IF NOT EXISTS sys_permission (
    id              BIGSERIAL    PRIMARY KEY,
    permission_code VARCHAR(100) NOT NULL,
    permission_name VARCHAR(100) NOT NULL,
    module          VARCHAR(50)  NOT NULL,
    api_path        VARCHAR(200) NOT NULL,
    api_method      VARCHAR(10)  NOT NULL DEFAULT 'GET',
    status          SMALLINT     NOT NULL DEFAULT 1,
    remark          VARCHAR(200),
    create_by       BIGINT       NOT NULL DEFAULT 0,
    create_time     TIMESTAMP(3),
    update_by       BIGINT       NOT NULL DEFAULT 0,
    update_time     TIMESTAMP(3),
    CONSTRAINT uk_sys_permission_code UNIQUE (permission_code)
);
CREATE INDEX IF NOT EXISTS idx_sys_permission_module ON sys_permission(module);
CREATE INDEX IF NOT EXISTS idx_sys_permission_status ON sys_permission(status);

COMMENT ON TABLE sys_permission IS '权限点表（非 Casbin 策略表）';
COMMENT ON COLUMN sys_permission.permission_code IS '权限编码，如 admin:list';
COMMENT ON COLUMN sys_permission.module IS '所属模块';
COMMENT ON COLUMN sys_permission.api_path IS '后端接口路径';
COMMENT ON COLUMN sys_permission.api_method IS '请求方法 GET/POST';
COMMENT ON COLUMN sys_permission.status IS '状态：0禁用 1启用';

-- 4. 菜单表
CREATE TABLE IF NOT EXISTS sys_menus (
    id              BIGSERIAL    PRIMARY KEY,
    permission_code VARCHAR(100),
    title           VARCHAR(50)  NOT NULL,
    parent_id       BIGINT       NOT NULL DEFAULT 0,
    type            SMALLINT     NOT NULL DEFAULT 2,
    path            VARCHAR(100) NOT NULL DEFAULT '',
    component       VARCHAR(255) NOT NULL DEFAULT '',
    external_url    VARCHAR(300) NOT NULL DEFAULT '',
    icon            VARCHAR(50)  NOT NULL DEFAULT '',
    status          SMALLINT     NOT NULL DEFAULT 1,
    is_hidden       SMALLINT     NOT NULL DEFAULT 0,
    is_public       SMALLINT     NOT NULL DEFAULT 0,
    is_system       SMALLINT     NOT NULL DEFAULT 0,
    sort_order      INTEGER      NOT NULL DEFAULT 0,
    remark          VARCHAR(200),
    create_by       BIGINT       NOT NULL DEFAULT 0,
    create_time     TIMESTAMP(3),
    update_by       BIGINT       NOT NULL DEFAULT 0,
    update_time     TIMESTAMP(3),
    deleted_time    TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_sys_menus_parent_id ON sys_menus(parent_id);

COMMENT ON TABLE sys_menus IS '菜单表';
COMMENT ON COLUMN sys_menus.permission_code IS '关联权限编码';
COMMENT ON COLUMN sys_menus.type IS '类型：1目录 2菜单 3按钮 4iframe 5外链';
COMMENT ON COLUMN sys_menus.status IS '状态：0禁用 1启用';
COMMENT ON COLUMN sys_menus.is_hidden IS '是否隐藏：0否 1是';
COMMENT ON COLUMN sys_menus.deleted_time IS '软删除时间';

-- 5. 部门表
CREATE TABLE IF NOT EXISTS sys_dept (
    id          BIGSERIAL    PRIMARY KEY,
    parent_id   BIGINT       NOT NULL DEFAULT 0,
    ancestors   VARCHAR(500) NOT NULL DEFAULT '',
    dept_name   VARCHAR(100) NOT NULL,
    dept_code   VARCHAR(50)  NOT NULL,
    leader_id   BIGINT,
    sort_order  INTEGER      NOT NULL DEFAULT 0,
    status      SMALLINT     NOT NULL DEFAULT 1,
    remark      VARCHAR(200),
    create_by   BIGINT       NOT NULL DEFAULT 0,
    create_time TIMESTAMP(3),
    update_by   BIGINT       NOT NULL DEFAULT 0,
    update_time TIMESTAMP(3),
    CONSTRAINT uk_sys_dept_code UNIQUE (dept_code)
);

COMMENT ON TABLE sys_dept IS '部门表';
COMMENT ON COLUMN sys_dept.ancestors IS '祖先ID链，如 ,0,1,2,';
COMMENT ON COLUMN sys_dept.leader_id IS '部门负责人ID';

-- 6. 数据规则表
CREATE TABLE IF NOT EXISTS sys_rule (
    id          BIGSERIAL    PRIMARY KEY,
    rule_name   VARCHAR(100) NOT NULL,
    domain      VARCHAR(50)  NOT NULL,
    config      JSONB        NOT NULL DEFAULT '{}',
    status      SMALLINT     NOT NULL DEFAULT 1,
    remark      VARCHAR(200),
    create_by   BIGINT       NOT NULL DEFAULT 0,
    create_time TIMESTAMP(3),
    update_by   BIGINT       NOT NULL DEFAULT 0,
    update_time TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_sys_rule_domain ON sys_rule(domain);
CREATE INDEX IF NOT EXISTS idx_sys_rule_status ON sys_rule(status);

COMMENT ON TABLE sys_rule IS '数据规则表';
COMMENT ON COLUMN sys_rule.rule_name IS '规则名称';
COMMENT ON COLUMN sys_rule.domain IS '规则领域';
COMMENT ON COLUMN sys_rule.config IS '规则配置（JSON）';

-- 7. 数据规则分配表
CREATE TABLE IF NOT EXISTS sys_rule_assignment (
    id           BIGSERIAL    PRIMARY KEY,
    rule_id      BIGINT       NOT NULL,
    target_type  SMALLINT     NOT NULL,
    target_id    BIGINT       NOT NULL,
    target_scope SMALLINT     NOT NULL DEFAULT 0,
    create_by    BIGINT       NOT NULL DEFAULT 0,
    create_time  TIMESTAMP(3)
);
CREATE INDEX IF NOT EXISTS idx_sys_rule_assignment_rule_id ON sys_rule_assignment(rule_id);
CREATE INDEX IF NOT EXISTS idx_sys_rule_assignment_target_type ON sys_rule_assignment(target_type);
CREATE INDEX IF NOT EXISTS idx_sys_rule_assignment_target_id ON sys_rule_assignment(target_id);

COMMENT ON TABLE sys_rule_assignment IS '数据规则分配表';
COMMENT ON COLUMN sys_rule_assignment.target_type IS '目标类型：1角色 2用户 3部门';
COMMENT ON COLUMN sys_rule_assignment.target_scope IS '作用范围：0仅本部门 1本部门及子部门';