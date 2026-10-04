-- 512 · ai_session / ai_event：AI 会话的事件日志真源与压缩投影。
--
-- 形态：会话（ai_session）持有计数、当前模型与压缩次数；事件（ai_event）是 append-only
-- 日志，压缩不改写历史、只追加一条折叠指令；**当前上下文 = 事件按 surface_op 投影的结果**。
--
-- 为什么不把「消息」当主表（就地更新）：
--   · 折叠必须可逆、可审计：摘要是模型写的一段文本，原文永远留着（就地删就再也搜不回来）；
--   · 前缀稳定：只追加才能让历史前缀字节不变，上游的前缀缓存才命中（口径见 docs/16）；
--   · 就地改写会丢掉「当时到底给模型看了什么」，出事故无法复盘。
--
-- surface_op 是本表唯一的「可见性改写」手段：
--   · append  —— 本条事件进入投影（kind 决定它是用户消息、助手回复、工具结果还是提示）；
--   · replace —— 不直接进投影，而是把 [replace_from_seq, replace_to_seq] 这段序号替换成
--     本条事件（kind = compact_summary 的摘要文本）。原文行仍在表里，decompress 靠它还原。
--
-- provider_key / model_id 存**字符串标识**而非外键：供应商被删除或改名时，历史会话不该
-- 因为悬空外键而删不掉、也不该在下次投影时读不出当时用的是哪个模型。可读的标识也让
-- 会话回放与日志排查不必再联表。
--
-- 计费与调用日志不在本批范围（会话与事件是「上下文怎么长大、怎么被折叠」的唯一真源）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / 独立 COMMENT ON，
-- 重复执行安全；带 TableName（ai_session 存在即整批跳过，见 register_ai_session.go），
-- 条件判据只枚举本批自己的对象；将来改结构要另开新迁移。

CREATE TABLE IF NOT EXISTS ai_session (
    id             BIGSERIAL    PRIMARY KEY,
    session_key    VARCHAR(120) NOT NULL,
    title          VARCHAR(200) NOT NULL DEFAULT '',
    provider_key   VARCHAR(50)  NOT NULL DEFAULT '',
    model_id       VARCHAR(120) NOT NULL DEFAULT '',
    status         SMALLINT     NOT NULL DEFAULT 1,
    next_seq       BIGINT       NOT NULL DEFAULT 1,
    head_seq       BIGINT       NOT NULL DEFAULT 0,
    compact_count  INT          NOT NULL DEFAULT 0,
    context_tokens BIGINT       NOT NULL DEFAULT 0,
    version        BIGINT       NOT NULL DEFAULT 1,
    create_by      BIGINT       NOT NULL DEFAULT 0,
    create_time    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    update_by      BIGINT       NOT NULL DEFAULT 0,
    update_time    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uk_ai_session_key UNIQUE (session_key)
);

CREATE TABLE IF NOT EXISTS ai_event (
    id               BIGSERIAL   PRIMARY KEY,
    session_id       BIGINT      NOT NULL,
    seq              BIGINT      NOT NULL,
    kind             VARCHAR(30) NOT NULL,
    surface_op       VARCHAR(20) NOT NULL DEFAULT 'append',
    replace_from_seq BIGINT      NOT NULL DEFAULT 0,
    replace_to_seq   BIGINT      NOT NULL DEFAULT 0,
    content          TEXT        NOT NULL DEFAULT '',
    content_tokens   BIGINT      NOT NULL DEFAULT 0,
    meta             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    create_time      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uk_ai_event_session_seq UNIQUE (session_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_ai_session_status_time ON ai_session (status, update_time DESC);
CREATE INDEX IF NOT EXISTS idx_ai_event_session_kind ON ai_event (session_id, kind);

COMMENT ON TABLE  ai_session IS 'AI 会话：持有序号分配、当前模型与压缩计数；内容是 ai_event 的投影';
COMMENT ON COLUMN ai_session.session_key IS '会话标识（唯一）：客户端给什么就存什么（不 hash），同一 key 再来即续写同一会话';
COMMENT ON COLUMN ai_session.title IS '会话标题（取首条用户消息的摘要，仅用于列表展示）';
COMMENT ON COLUMN ai_session.provider_key IS '当前供应商标识（对应 ai_provider.provider_key；存标识不存外键，供应商删除/改名不影响历史会话）';
COMMENT ON COLUMN ai_session.model_id IS '当前模型标识（供应商模型目录里的 id，如 deepseek/deepseek-v4.1-flash）';
COMMENT ON COLUMN ai_session.status IS '状态：1 进行中 / 0 归档';
COMMENT ON COLUMN ai_session.next_seq IS '下一个待分配的事件序号（同一会话内从 1 起递增，分配必须原子，见 ai_session.next_seq 的原子自增口径）';
COMMENT ON COLUMN ai_session.head_seq IS '投影可见范围的末尾序号（= 已分配的最大 seq；仅用于展示与增量拉取，不作为真源）';
COMMENT ON COLUMN ai_session.compact_count IS '已执行的压缩次数（每次成功的折叠 +1，用于前端提示与可观测）';
COMMENT ON COLUMN ai_session.context_tokens IS '当前投影的估算 token 数（每次 append / 折叠后重算；仅用于触发判断与展示，不参与计费）';
COMMENT ON COLUMN ai_session.version IS '乐观锁版本号：会话级读-改-写（改标题 / 切模型 / 归档）必须带 version 条件';
COMMENT ON COLUMN ai_session.create_by IS '创建人 ID（与 sys_* 家族同口径）';
COMMENT ON COLUMN ai_session.update_by IS '最后修改人 ID';

COMMENT ON TABLE  ai_event IS 'AI 会话事件日志（append-only 真源）：压缩只追加折叠指令，原文永不删除';
COMMENT ON COLUMN ai_event.session_id IS '所属会话 ID';
COMMENT ON COLUMN ai_event.seq IS '会话内序号（从 1 起、无洞；唯一约束兼作并发插入的互斥）';
COMMENT ON COLUMN ai_event.kind IS '事件类别：user / assistant / tool / compact_start / compact_summary / compact_end / note';
COMMENT ON COLUMN ai_event.surface_op IS '可见性操作：append 进投影；replace 把 [replace_from_seq,replace_to_seq] 段替换成本条';
COMMENT ON COLUMN ai_event.replace_from_seq IS 'surface_op=replace 时的起始序号（含），否则为 0';
COMMENT ON COLUMN ai_event.replace_to_seq IS 'surface_op=replace 时的结束序号（含），否则为 0';
COMMENT ON COLUMN ai_event.content IS '事件正文（原文；压缩摘要也写在这里，原文不删）';
COMMENT ON COLUMN ai_event.content_tokens IS '本条正文的估算 token 数（用于计量与折叠净收益计算）';
COMMENT ON COLUMN ai_event.meta IS '附加信息 JSON：工具名/参数摘要/模型/耗时/错误码/折叠前后 token 等，键由代码白名单约束';
