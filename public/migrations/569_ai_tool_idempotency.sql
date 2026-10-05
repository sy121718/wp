-- 569 · ai_tool_idempotency：写工具的幂等台账（docs/17 D8 的「写操作三件套」之一）。
--
-- 背景：工具层的写操作由模型发起、可能被重试（超时 / 上下文重建 / 用户重发）。
--   没有幂等键时一次「新建文章」的意图可能落两篇，而两篇都合法、没有任何错误 ——
--   这类故障只有人来核对时才会发现。
--
-- 为什么在库里而不是进程内存：服务重启后内存里的键全丢，而调用方的重试**恰恰
--   发生在超时之后** —— 那正是最可能重启的时刻。放在内存里等于在最需要它的时候失效。
--
-- 键是 (tool_name, idem_key)：不同工具的同一个 uuid 互不干扰。
--   idem_key 由**调用方**给（见 internal/mcp/write.go 的说明）：生成权在服务端的话，
--   每次重试都会拿到新键，等于没有幂等。
--
-- result_text / result_data 存的是**上次成功的结果**：命中时直接返回它，
--   这样模型看到的返回值与第一次一致（否则它会以为「又改了一次」而汇报两次改动）。
--   **只记成功**：失败不落库，让重试能真的重试。
--
-- 过期：本表只用于挡「短时间内的重复提交」，24 小时足够 —— 更久的重试要么是
--   人为操作（那本来就该当成一次新意图），要么早就被别的手段拦住了。
--   清理由 Lookup 的时间窗口负责（不引入定时任务），历史行定期人工清即可。
--
-- 幂等：CREATE TABLE IF NOT EXISTS + 唯一约束。

CREATE TABLE IF NOT EXISTS ai_tool_idempotency (
    id          BIGSERIAL   PRIMARY KEY,
    tool_name   TEXT        NOT NULL,
    idem_key    TEXT        NOT NULL,
    result_text TEXT        NOT NULL DEFAULT '',
    result_data JSONB,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uk_ai_tool_idempotency UNIQUE (tool_name, idem_key)
);

COMMENT ON TABLE ai_tool_idempotency IS '写工具幂等台账：同一次意图的重试返回上次成功的结果';
COMMENT ON COLUMN ai_tool_idempotency.idem_key IS '调用方给的幂等键（同一次意图复用同一个值）';
