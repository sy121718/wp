-- 466 · 评论（BIZ-5）：一张表服务多种实体。
--
-- 为什么是**独立模块**而不是每个实体模块自带一张评论表：评论的横切关注点
-- （审核状态机 / 限流 / 防刷 / 审核后台 / i18n / 分页）与实体无关。每模块自带等于把这一整套
-- 写 N 遍，运营还要跑 N 个后台页；而「挂到新实体上」这件事本身只需要一行注册
-- （见 internal/module/comment/contract 的 EntityType）。
--
-- 多态挂载 = entity_type + entity_id：
--   · entity_type 是**字符串**而不是 PG enum —— 取值白名单由**拥有该实体的模块**声明，
--     comment 模块不认识 article / product 的任何细节（跨模块只走 contract + 不可变 DTO）。
--     PG enum 要求「新增取值必须改这个类型」，于是每加一种可评论实体都要写一条迁移，
--     而白名单的真源却在 Go 侧 —— 两份真源必然漂移（AGENTS.md §数据库 的同型判据：
--     白名单由拥有该表的实体声明，不要另抄一份）。
--   · 同理**不建 CHECK 约束**枚举 entity_type：约束一旦写死，把评论挂到新实体上就要
--     停服改约束。合法性在写入入口按**注册表**判定（fail-closed：未注册的类型一律拒绝）。
--   · entity_id 是**字符串**而不是 uuid：不同实体的 id 形态不同（商品是 uuid，
--     将来可能是大整数），统一字符串避免「uuid 列装不下别的实体 id」。
--     代价是没有类型层保证 —— 由写入入口做形状校验（长度 + 字符集）。
--
-- 主键选型：BIGSERIAL（bigint identity）。判据（AGENTS.md §数据库）：对外实体用 uuid、
-- 纯内部流水用 bigint。评论 id **不出现在系统边界之外** —— 列表与提交的接口都不需要它，
-- 后台审核页是控制面（用 id 只是为了提交表单），因此没有「猜 id 越权读别人的数据」这条路。
-- 若将来出现「按 id 删自己的评论」这类把 id 摆到 URL 上的需求，必须同时换 uuid 并加归属校验。
--
-- 状态机：pending（落库默认）→ approved（可见） / rejected（驳回） / spam（判为垃圾）。
-- 产品口径是**先审后发**：列表只出 approved，pending 不进任何公开面。
--
-- 回复：parent_id 自引用**只支持一级**（回复的回复也挂到顶层评论上，由 service 归一），
-- 不做多级嵌套 —— 嵌套深度是展示层的复杂度，一级已经覆盖「回应某条评论」的实际需求。
-- 刻意不建外键（同 462 / 460 等台账口径：硬失败会卡住「父评论已删、子回复待回收」的清理）。
--
-- RLS（DB-009）：评论按工程隔离（列表 / 审核 / 提交都带 project_id），策略在建表处**同批**铺，
-- 谓词与迁移 199 / 215 / 462 逐字一致。为什么必须同批：策略不存在时 ENABLE RLS 的表
-- 对所有非属主连接**一行都不可见**（默认 deny）—— model 里包了 InProjectScope 也救不回来。
--
-- 幂等：CREATE TABLE / INDEX / POLICY 全带 IF NOT EXISTS 或先 DROP，重复执行安全。
-- 注册见 register_comment.go（新表，用默认「表存在即跳过」检查）。
--
-- ⚠ 写完用 psql 事务回滚做过静态 + 语义校验：
--   psql -U root -d wp -v ON_ERROR_STOP=1 -c "BEGIN;" -f 466_comments.sql -c "ROLLBACK;"
-- （PG 的 PREPARE 只接受 SELECT/DML，写 DDL 会 syntax error at or near "CREATE"；
-- 事务回滚是更完整的分析 —— 数据库会做语法与表 / 列语义检查，而 ROLLBACK 不留痕。）

CREATE TABLE IF NOT EXISTS comments (
    id             BIGSERIAL   PRIMARY KEY,
    -- 工程归属：评论按工程隔离（同 membership 口径），列表 / 审核 / 提交都必须给工程。
    project_id     UUID        NOT NULL,
    -- 被评论实体的类型（白名单由拥有该实体的模块声明，写入入口按注册表判定）。
    entity_type    VARCHAR(40) NOT NULL,
    -- 被评论实体的 id（字符串：商品是 uuid、将来可能有大整数，统一字符串避免列装不下）。
    entity_id      VARCHAR(64) NOT NULL,
    -- 评论者 = 访客账号 id（users.id）。产品口径：匿名不可评，所以恒非空。
    -- 刻意不建外键（同 462：评论不该因账号注销而消失）。
    user_id        BIGINT      NOT NULL,
    -- 回复目标（顶层评论的 id）；NULL = 顶层评论。只支持一级回复，多级由 service 归一。
    parent_id      BIGINT,
    -- 评论正文（纯文本；长度上限由 service 与 CHECK 双层把关，展示层由 Jet 默认转义）。
    body           TEXT        NOT NULL,
    -- 审核状态：pending=待审（新评论默认）/ approved=已通过（唯一会出现在公开列表里的）/ rejected=驳回 / spam=垃圾。
    status         VARCHAR(16) NOT NULL DEFAULT 'pending',
    -- 来源 IP 的**带盐哈希**（不存明文，同 analytics 口径）；空串 = 本次请求拿不到 IP。
    author_ip_hash VARCHAR(64) NOT NULL DEFAULT '',
    -- 审核人（sys_admin.id）与审核时刻；未审核为 NULL。「谁来审的」是审核队列的追责依据。
    reviewed_by    BIGINT,
    reviewed_at    TIMESTAMPTZ,
    create_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);

-- 状态取值约束：四个取值是**封闭**的（状态机由本模块独占，拥有者之外没人会加取值），
-- 与 entity_type 的区别正在于此 —— 那个的取值来自别的模块，这个的取值来自本模块。
ALTER TABLE comments DROP CONSTRAINT IF EXISTS ck_comments_status;
ALTER TABLE comments ADD CONSTRAINT ck_comments_status
    CHECK (status IN ('pending', 'approved', 'rejected', 'spam'));

-- 正文长度约束（最后一道）。上限与 Go 侧常量必须一致，否则表现是「service 放行、数据库报错」，
-- 而那条报错会被归口成「系统内部错误」（用户看到的是「评论发不出去」，无从知道是长度）。
ALTER TABLE comments DROP CONSTRAINT IF EXISTS ck_comments_body_len;
ALTER TABLE comments ADD CONSTRAINT ck_comments_body_len
    CHECK (char_length(body) BETWEEN 1 AND 2000);

-- 列表主查询：按 (工程, 实体类型, 实体 id, 状态) 取最近 N 条（公开列表恒 status='approved'）。
-- create_time DESC 进索引：分页按时间倒序，没有它就得排序整段。
CREATE INDEX IF NOT EXISTS idx_comments_entity_list
    ON comments (project_id, entity_type, entity_id, status, create_time DESC)
    WHERE deleted_at IS NULL;

-- 后台审核队列：按状态取最老的（先到先审），跨工程（后台页会带上工程筛选条件，
-- 但队列本身的驱动顺序是「状态 + 时间」，不带工程维度）。
CREATE INDEX IF NOT EXISTS idx_comments_review_queue
    ON comments (status, create_time DESC)
    WHERE deleted_at IS NULL;

-- 回复反查（某条评论下面有哪些回复）；同时服务「父评论已删、子回复待处理」的清理。
CREATE INDEX IF NOT EXISTS idx_comments_parent
    ON comments (parent_id)
    WHERE deleted_at IS NULL;

-- 同一个人在同一实体下的刷屏复查（防刷的事后排查口，不是限流本身）。
CREATE INDEX IF NOT EXISTS idx_comments_author_recent
    ON comments (project_id, user_id, create_time DESC)
    WHERE deleted_at IS NULL;

-- 工程隔离策略（DB-009）：谓词与迁移 199 / 215 / 462 逐字一致。
-- 未设 app.project_id 时 current_setting(..., true) 返回 NULL ⇒ 谓词为 NULL ⇒ 行不可见（fail closed）。
ALTER TABLE comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE comments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_comments_project ON comments;
CREATE POLICY p_comments_project ON comments
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

COMMENT ON TABLE comments IS '评论（BIZ-5）：多态挂载（entity_type + entity_id），先审后发（列表只出 approved），支持一级回复；project_id 受 RLS 隔离';
COMMENT ON COLUMN comments.id IS '纯内部流水主键（不对外暴露：列表与提交接口都不需要它；将来若把 id 摆进 URL 必须改 uuid 并加归属校验）';
COMMENT ON COLUMN comments.project_id IS '工程归属：评论按工程隔离，列表 / 审核 / 提交都必须给工程';
COMMENT ON COLUMN comments.entity_type IS '被评论实体类型：取值白名单由拥有该实体的模块声明（comment 模块不认识具体实体），未注册的类型在写入入口被拒绝';
COMMENT ON COLUMN comments.entity_id IS '被评论实体 id：字符串存储（商品是 uuid、将来可能有大整数，统一字符串避免 uuid 列装不下别的实体 id）';
COMMENT ON COLUMN comments.user_id IS '评论者 = 访客账号 id（users.id）；匿名不可评，故恒非空；刻意不建外键（评论不该因账号注销而消失）';
COMMENT ON COLUMN comments.parent_id IS '回复目标（顶层评论 id）；NULL = 顶层。只支持一级回复：回复的回复由 service 归一到顶层评论之下';
COMMENT ON COLUMN comments.body IS '评论正文（纯文本，1..2000 字符，与 Go 侧上限一致）；展示层由 Jet 默认转义，不做 HTML 渲染';
COMMENT ON COLUMN comments.status IS '审核状态：pending=待审（新评论默认，不进任何公开面）/ approved=已通过（唯一可见）/ rejected=驳回 / spam=垃圾';
COMMENT ON COLUMN comments.author_ip_hash IS '来源 IP 的带盐哈希（不存明文，同 analytics 口径）；空串 = 本次请求拿不到 IP；用途只有防刷排查';
COMMENT ON COLUMN comments.reviewed_by IS '审核人 = sys_admin.id；未审核为 NULL。审核队列的追责依据';
COMMENT ON COLUMN comments.reviewed_at IS '审核时刻；未审核为 NULL';
