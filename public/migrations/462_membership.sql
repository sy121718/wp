-- 462 · 会员等级与权益（BIZ-3）。
--
-- 三张表回答三个不同的问题，各自独立：
--   · membership_tiers          —— 这个工程有哪些等级、门槛是多少、哪个是默认等级；
--   · membership_entitlements   —— 某个等级享有哪些权益（本批两种：免运费 / 折扣）；
--   · membership_assignments    —— 某个访客账号在当前工程属于哪个等级。
--
-- 为什么是独立模块而不是塞进 user：AGENTS.md 的命名约束把 admin / user 定义为两个独立领域，
-- 且 doc 06-A §3 表 #6 的「依赖」列写的是「admin 访客账号领域」（依赖而非属于）、
-- docs/13-module-inventory.md 的 user 行「不负责」列已预留「会员等级 / 权益（BIZ-3）」切口。
-- 更直接的判据是消费方：等级要被 order（折扣）、cart（运费）、runtimefragment（展示）消费，
-- 塞进 user 会让片段层拿到含注册 / 改密 / 踢设备的完整 UserService，违反本仓既有的收窄思路。
--
-- 消费额口径按**工程**：同一个访客账号在两个工程可以分别是白银与黄金 ——
-- 消费发生在工程内的订单上，跨工程合并消费额会把两个独立站点的会员体系搅在一起。
-- 因此 membership_tiers.project_id 进唯一键，assignments 的主键也是 (project_id, user_id)。
--
-- 金额单位：threshold_amount 是 **BIGINT 分**，与 orders 的金额列同单位。
-- 全库口径是 orders 用分、products 用 numeric(12,2) 元，换算发生在商品域边界；
-- 本表参与「消费额 ≥ 门槛」的比较，必须与 orders 同侧，否则会出现「差 100 倍」的静默错档。
--
-- 刻意不建的三样东西：
--   · **不建流水表** —— 自动归属可以从 orders 重算（不是不可推导的真源），
--     手工变更的留痕复用 masterdata.RecordChangesTx（本批不做，第二阶段）；
--   · **不建 users 外键** —— 全库形态是应用层维护（123_user.sql 的 user_profiles.user_id 同样无外键），
--     且会员归属不该因账号注销而消失；
--   · **不建 tiers 外键到 entitlements / assignments** —— 与 page_schedules 等台账同口径，
--     硬失败会卡住「等级已删、关联行待回收」的清理。
--
-- RLS（DB-009）：两张带 project_id 的表在建表处**同批**铺策略（ENABLE + FORCE + POLICY），
-- 谓词与迁移 199 / 215 逐字一致。为什么不在 215 里补：历史迁移保持原样（AGENTS.md §数据库），
-- 而新表的隔离必须与表同时到达 —— 否则「表已上线、策略还没铺」的那段时间里，
-- model 里包了 InProjectScope 也一行都挡不住（策略不存在 = 谓词不参与判定）。
-- membership_entitlements 没有 project_id（它的作用域经 tier_id 传递），不在 RLS 清单里 ——
-- 与 page_schedules 等「同族表口径一致」的理由相同。
--
-- 幂等：CREATE TABLE / INDEX / POLICY 全带 IF NOT EXISTS 或先 DROP，重复执行安全。
-- 注册见 register_membership.go（新表，用默认「表存在即跳过」检查）。
--
-- ⚠ 本文件写完后用 psql 事务回滚做过静态 + 语义校验：
--   psql -U root -d wp -v ON_ERROR_STOP=1 -c "BEGIN;" -f 462_membership.sql -c "ROLLBACK;"
-- （PG 的 PREPARE 只接受 SELECT/DML，写 DDL 会 syntax error；事务回滚是更完整的分析。）

-- 1. membership_tiers —— 等级定义
CREATE TABLE IF NOT EXISTS membership_tiers (
    id               BIGSERIAL   PRIMARY KEY,
    -- 工程归属：等级按工程定义，同一访客在不同工程可以有不同等级。
    project_id       UUID        NOT NULL,
    name             VARCHAR(60) NOT NULL,
    -- 等级高低：**越大越高**。解析「消费额落在哪一档」时按它降序取第一个满足门槛的档，
    -- 因此它是排序真源而不是 threshold_amount（门槛可以相同之外的任意关系，排序值由运营控制）。
    sort_order       INTEGER     NOT NULL DEFAULT 0,
    -- 升级门槛（**分**）：该工程内累计消费额达到此值即进入本档。默认等级的门槛恒为 0。
    threshold_amount BIGINT      NOT NULL DEFAULT 0,
    -- 每工程恰一个默认等级：它在「该访客还没有归属行」与「没有任何门槛被满足」两种情况下兜底。
    is_default       BOOLEAN     NOT NULL DEFAULT FALSE,
    remark           VARCHAR(200) NOT NULL DEFAULT '',
    create_time      TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

-- 同工程内等级名不重复（软删除行不占键）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_tiers_name
    ON membership_tiers (project_id, name) WHERE deleted_at IS NULL;

-- 门槛不许撞：两个非默认等级门槛相同，会让「满 1000 是白银还是黄金」取决于查询顺序，
-- 而**没有任何报错** —— 同一个访客的等级会随着索引扫描顺序漂移。宁可建不上让人改门槛。
-- is_default = FALSE：默认等级的门槛恒为 0，与「0 门槛的普通档」重复是正常建站形态。
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_tiers_threshold
    ON membership_tiers (project_id, threshold_amount)
    WHERE deleted_at IS NULL AND is_default = FALSE;

-- 每工程恰一个默认等级（部分唯一索引的谓词直接在数据库层承载这条不变量）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_tiers_default
    ON membership_tiers (project_id) WHERE is_default AND deleted_at IS NULL;

-- 解析路径的驱动索引：按工程取全部等级、sort_order 降序（等级解析与后台列表共用）。
CREATE INDEX IF NOT EXISTS idx_membership_tiers_project_sort
    ON membership_tiers (project_id, sort_order DESC) WHERE deleted_at IS NULL;

-- 2. membership_entitlements —— 权益
--
-- 用**强类型列 + CHECK** 而不是 JSONB：这张表直接参与金额计算，
-- `{"percent":"20"}` 或负折扣值在 Go 侧类型断言会算成负数或直接 panic，
-- 而列型 + CHECK 比 Go 层校验更难绕过（应用层被改一处就漏，数据库约束是唯一的那道）。
-- 一期交付两种 kind，表结构一次到位：free_shipping（免运费）/ discount（折扣百分比）。
CREATE TABLE IF NOT EXISTS membership_entitlements (
    id         BIGSERIAL   PRIMARY KEY,
    tier_id    BIGINT      NOT NULL,
    kind       VARCHAR(32) NOT NULL,
    -- 按 kind 解释的整数值：
    --   free_shipping —— 0 / 1（是否免运费）
    --   discount      —— 1..100（折扣百分比，20 表示打八折，即扣减 20%）
    value_int  BIGINT      NOT NULL,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 一个等级的一种权益只有一条（改权益是 upsert 而不是追加）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_entitlements_tier_kind
    ON membership_entitlements (tier_id, kind);

-- 取值域由 CHECK 承载，按 kind 分别限值：
-- 负的百分比会把折扣算成**加价**，超过 100 会让应付为负 —— 两者都不该靠调用方自觉避免。
ALTER TABLE membership_entitlements DROP CONSTRAINT IF EXISTS ck_membership_entitlements_value;
ALTER TABLE membership_entitlements ADD CONSTRAINT ck_membership_entitlements_value CHECK (
    (kind = 'free_shipping' AND value_int IN (0, 1))
    OR (kind = 'discount' AND value_int BETWEEN 1 AND 100)
);

-- 3. membership_assignments —— 归属
CREATE TABLE IF NOT EXISTS membership_assignments (
    id          BIGSERIAL   PRIMARY KEY,
    project_id  UUID        NOT NULL,
    -- 访客账号 id（users.id）。不建外键：全库形态是应用层维护，且归属不该因注销而消失。
    user_id     BIGINT      NOT NULL,
    tier_id     BIGINT      NOT NULL,
    -- auto = 日结按消费额重算得出；manual = 后台手工指定。
    -- 手工指定的行**不被自动重算覆盖**，语义由 model 的原子 SQL（WHERE source <> 'manual'）承载，
    -- 不靠调用方记得跳过 —— 后者会在新增一个调用点时静默失效。
    source      VARCHAR(16) NOT NULL,
    -- 归属生效时刻（业务时刻，与 orders.paid_at / page_schedules.scheduled_at 同口径的 *_at 命名；
    -- 管理时间列另有 create_time / update_time）。
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_membership_assignments_source CHECK (source IN ('auto', 'manual'))
);

-- 一个访客在一个工程只能有一个当前等级。
-- 两个同时生效的行 = 折扣被算两次，而页面看起来完全正常（两行都在、都合法）——
-- 这是本表最危险的静默故障，所以键建在数据库上而不是靠 service 的查询习惯。
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_assignments_project_user
    ON membership_assignments (project_id, user_id);

-- 等级 → 归属的反查（后台「这个等级里有谁」与删除前的引用检查）。
CREATE INDEX IF NOT EXISTS idx_membership_assignments_project_tier
    ON membership_assignments (project_id, tier_id);

-- 4. 工程隔离策略（DB-009）：谓词与迁移 199 / 215 逐字一致。
-- 未设 app.project_id 时 current_setting(..., true) 返回 NULL ⇒ 谓词为 NULL ⇒ 行不可见（fail closed）。
ALTER TABLE membership_tiers ENABLE ROW LEVEL SECURITY;
ALTER TABLE membership_tiers FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_membership_tiers_project ON membership_tiers;
CREATE POLICY p_membership_tiers_project ON membership_tiers
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

ALTER TABLE membership_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE membership_assignments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_membership_assignments_project ON membership_assignments;
CREATE POLICY p_membership_assignments_project ON membership_assignments
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

-- 5. orders 的会员折扣列（决策 4：折扣与券**相加扣减**，各自独立计账）。
--
-- 为什么不复用 discount_total：它有一条 SEC-001 既有判据「无券时折扣恒为 0」，
-- 把会员折扣并进去会直接破坏那条判据，且对账时无法分辨「券减了多少 / 会员减了多少」。
-- 两列相加即总扣减，两个来源各自可审计。
ALTER TABLE orders ADD COLUMN IF NOT EXISTS membership_discount_total BIGINT NOT NULL DEFAULT 0;

COMMENT ON TABLE membership_tiers IS '会员等级定义（BIZ-3）：按工程建键，threshold_amount 单位分（与 orders 同口径），sort_order 越大越高，每工程恰一个 is_default 等级';
COMMENT ON COLUMN membership_tiers.project_id IS '工程归属：等级按工程定义，同一访客在不同工程可有不同等级（消费额口径本身也按工程）';
COMMENT ON COLUMN membership_tiers.sort_order IS '等级高低，越大越高；解析时按它降序取第一个满足门槛的档（排序真源，不是 threshold_amount）';
COMMENT ON COLUMN membership_tiers.threshold_amount IS '升级门槛（分）：工程内累计消费额达到此值进入本档；默认等级恒为 0；非默认档之间不许相同（唯一索引）';
COMMENT ON COLUMN membership_tiers.is_default IS '每工程恰一个默认等级（部分唯一索引承载）：无归属行与无门槛满足两种情况都回退到它';
COMMENT ON TABLE membership_entitlements IS '等级权益（BIZ-3）：一期两种 kind（free_shipping / discount），整数列 + CHECK 限值而不是 JSONB（本表参与金额计算）';
COMMENT ON COLUMN membership_entitlements.tier_id IS '所属等级（membership_tiers.id）；刻意不建外键，与 page_schedules 等台账同口径（硬失败会卡住已删等级的关联行回收）';
COMMENT ON COLUMN membership_entitlements.kind IS '权益类型：free_shipping=免运费；discount=折扣百分比。唯一键 (tier_id, kind)：改权益是 upsert，不是追加';
COMMENT ON COLUMN membership_entitlements.value_int IS '按 kind 解释：free_shipping 取 0/1；discount 取 1..100（20 = 打八折，即扣减 20%）。越界由 ck_membership_entitlements_value 拒绝';
COMMENT ON TABLE membership_assignments IS '会员归属（BIZ-3）：(project_id, user_id) 唯一 —— 一个访客在一个工程只能有一个当前等级';
COMMENT ON COLUMN membership_assignments.user_id IS '访客账号 id（users.id）；刻意不建外键（全库形态是应用层维护，且归属不该因账号注销而消失）';
COMMENT ON COLUMN membership_assignments.source IS 'auto=日结按消费额重算；manual=后台手工指定。手工行不被自动重算覆盖，语义由原子 SQL 的 WHERE source <> ''manual'' 承载';
COMMENT ON COLUMN membership_assignments.assigned_at IS '归属生效时刻（业务时刻，与 orders.paid_at 同族的 *_at 命名）；管理时间列另有 create_time / update_time';
COMMENT ON COLUMN orders.membership_discount_total IS '会员折扣金额（分，BIZ-3）：与 discount_total（券）相加即总扣减，两列各自独立计账 —— 不改 discount_total 的既有语义（SEC-001：无券时折扣恒为 0）';
