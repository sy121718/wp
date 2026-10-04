-- 541 · ai_tool_call_log：模型**工具调用**流水（谁、调了哪个工具、结论是什么、多久）。
--
-- 背景：540 之前 agent loop 已能跑工具（77/78 批），但「它到底调过什么」只散在
--   ai_event（正文）与结构化日志里：事件是对话真源（要参与投影与计量），
--   日志是排查用的流式文本。**审计**要的是可检索的一行一条：安全要回答
--   「谁在什么时候试图调用了什么、被拒了几次」，按会话读事件或翻日志都答不利索。
--
-- 为什么不并进 ai_call_log（537）：那张表的列是**出站大模型调用**的形状
--   （provider / model / protocol / token 用量），工具调用没有这些维度，
--   硬塞只能把 tool_name 写进 model_id 之类的错位。
--   两者相同的是写入纪律：旁路观测、协程异步写、写失败不影响业务。
--
-- 为什么存**摘要**而不是全文（arguments_summary / result_summary）：
--   · 工具参数的原文与结果全文**已经在 ai_event 里**（工具事件，append-only 真源），
--     要还原细节去读事件；审计表只负责「发生过、结论是什么、多大」。
--   · 全文可能含业务敏感数据（客户名单、订单明细），审计表的读面更宽（将来要给外部 /mcp 用）。
--   truncated 列标记这次结果是否被剪枝过 —— 模型当时看到的是剪枝后的版本，
--   排查「模型为什么没用到完整数据」时要看这一列，而不是去比对长度。
--
-- status 只记**结论分类**（ok / args_error / forbidden / failed），不放底层原文：
--   上游与工具的错误原文可能回显内部标识，原文只进日志。
-- error_key 是 i18n key（与 enums 哨兵同源），供后台直接展示可翻译文案。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / 独立 COMMENT ON，
--   重复执行安全；判据见 register_ai_tool_call_log.go（只判本批这一张表在不在）。

CREATE TABLE IF NOT EXISTS ai_tool_call_log (
    id                BIGSERIAL    PRIMARY KEY,
    session_id        BIGINT       NOT NULL DEFAULT 0,
    user_id           BIGINT       NOT NULL DEFAULT 0,
    tool_name         VARCHAR(80)  NOT NULL DEFAULT '',
    arguments_summary VARCHAR(500) NOT NULL DEFAULT '',
    result_summary    VARCHAR(500) NOT NULL DEFAULT '',
    result_len        BIGINT       NOT NULL DEFAULT 0,
    truncated         BOOLEAN      NOT NULL DEFAULT false,
    status            VARCHAR(20)  NOT NULL DEFAULT '',
    error_key         VARCHAR(80)  NOT NULL DEFAULT '',
    latency_ms        BIGINT       NOT NULL DEFAULT 0,
    create_time       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE ai_tool_call_log IS '模型工具调用流水：一次工具调用一行（旁路观测，不参与会话投影与计量）';
COMMENT ON COLUMN ai_tool_call_log.session_id IS '这次调用属于哪条会话；0 = 未记录';
COMMENT ON COLUMN ai_tool_call_log.user_id IS '发起这次调用的后台账号 id（服务端从登录态取，不采信请求参数）；0 = 未记录';
COMMENT ON COLUMN ai_tool_call_log.tool_name IS '被调用的工具名（注册表里的名字）';
COMMENT ON COLUMN ai_tool_call_log.arguments_summary IS '模型给的参数**摘要**（超长截断）；原文在 ai_event 的工具事件里';
COMMENT ON COLUMN ai_tool_call_log.result_summary IS '工具结果**摘要**（超长截断）；全文在 ai_event 的工具事件里';
COMMENT ON COLUMN ai_tool_call_log.result_len IS '结果原文长度（字符数）；summary 被截断时这里是全量长度';
COMMENT ON COLUMN ai_tool_call_log.truncated IS '交给模型的结果是否被剪枝过（模型看到的是剪枝后的版本）';
COMMENT ON COLUMN ai_tool_call_log.status IS 'ok / args_error / forbidden / failed（取值见 aienums.ToolCallStatus）';
COMMENT ON COLUMN ai_tool_call_log.error_key IS '失败时的 i18n key（与 enums 哨兵同源）；不存底层原文';
COMMENT ON COLUMN ai_tool_call_log.latency_ms IS '工具执行耗时（毫秒，含权限判定与参数校验）';

-- 三条读路径：会话详情看这条会话调过什么；「某个账号调了什么」；全局最近调用；
-- 另有「某工具被拒了多少次」这类安全查询走 (tool_name, status)。
CREATE INDEX IF NOT EXISTS idx_ai_tool_call_log_session ON ai_tool_call_log (session_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_ai_tool_call_log_user ON ai_tool_call_log (user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_ai_tool_call_log_time ON ai_tool_call_log (create_time DESC);
CREATE INDEX IF NOT EXISTS idx_ai_tool_call_log_tool_status ON ai_tool_call_log (tool_name, status);
