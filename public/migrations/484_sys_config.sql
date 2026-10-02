-- 484 · sys_config：系统配置表（按 group_key 分组存 JSON）＋ 乐观锁版本号。
--
-- 形态来自 go-mvc 的 013 设计（分组存储：group_key 唯一 + config_data 整组 JSON，
-- 见 sy121718/go-mvc public/migrations/013_sys_config.go），按 PostgreSQL 与本项目
-- sys_* 家族口径改写（BIGSERIAL / JSONB / SMALLINT / TIMESTAMPTZ / 独立 COMMENT ON）。
--
-- 定位（只放什么）：
--   · 只放**运行期可改的业务默认值** —— 全局默认语言、默认国家、默认货币、站点语言
--     URL 方案等；后台「系统设置」页是它唯一的写入口。
--   · **不放**基础设施配置（数据库连接 / Redis / session_secret / CORS 白名单）——
--     读 DB 的前提是知道 DB 在哪，搬进来即成循环依赖；那些留在 config.yaml。
--   · **不放**每工程各异的设置 —— 那些在 projects.settings（工程级覆盖）；
--     读取链：工程值（projects.settings）> 全局默认（本表）> 启动兜底（config.yaml）。
--   · **不放**只读选项数据 —— 地区 / 语言 / 货币是选项，在 sys_area / sys_dict。
--
-- 分组粒度 = 后台**一次保存的单元**（一个表单一次提交写一组）。
-- 乐观锁按组生效（version）：整组读-改-写必须带 version 条件，否则两个管理员改同组
-- 不同键时后写者静默覆盖前者（AGENTS「写操作的事务与回滚」：读-改-写必须有行锁或原子 SQL）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，重复执行安全。
-- 注册见 register_sys_reference.go：**本迁移刻意不带 TableName**，每次启动都会执行本文件全部语句 ——
-- 因此本文件**只能包含幂等语句**（CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / COMMENT ON）。
-- 禁止加入 ALTER TABLE … ADD COLUMN 这类非幂等语句（会每次启动重复执行）；将来改结构要另开新迁移。
-- 理由：带 TableName 的迁移在表已存在时整条跳过，文件里 COMMENT ON 的改动永不生效，库与文件静默漂移。

CREATE TABLE IF NOT EXISTS sys_config (
    id          BIGSERIAL    PRIMARY KEY,
    group_key   VARCHAR(50)  NOT NULL,
    group_name  VARCHAR(100) NOT NULL,
    config_data JSONB        NOT NULL,
    remark      VARCHAR(500),
    status      SMALLINT     NOT NULL DEFAULT 1,
    version     BIGINT       NOT NULL DEFAULT 1,
    create_by   BIGINT       NOT NULL DEFAULT 0,
    create_time TIMESTAMPTZ  NOT NULL DEFAULT now(),
    update_by   BIGINT       NOT NULL DEFAULT 0,
    update_time TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_sys_config_group_key UNIQUE (group_key)
);

CREATE INDEX IF NOT EXISTS idx_sys_config_status ON sys_config (status);

COMMENT ON TABLE  sys_config IS '系统配置表：按 group_key 分组存储 JSON 配置（全局默认值；工程级覆盖在 projects.settings）';
COMMENT ON COLUMN sys_config.group_key IS '配置分组键名（唯一）：分组粒度 = 后台一次保存的单元';
COMMENT ON COLUMN sys_config.group_name IS '配置分组显示名（后台页面展示用）';
COMMENT ON COLUMN sys_config.config_data IS '整组配置 JSON；组内键由代码白名单约束，解析失败一律回退默认值';
COMMENT ON COLUMN sys_config.remark IS '备注说明';
COMMENT ON COLUMN sys_config.status IS '状态：1 启用 / 0 禁用';
COMMENT ON COLUMN sys_config.version IS '乐观锁版本号：整组读-改-写必须带 version 条件，防丢更新';
COMMENT ON COLUMN sys_config.create_by IS '创建人 ID（与 sys_* 家族同口径）';
COMMENT ON COLUMN sys_config.update_by IS '最后修改人 ID';
