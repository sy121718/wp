-- 678 · 写工具幂等台账加调用者维度（审计 SEC-05，B02）
--
-- 缺陷：幂等键原先是 (tool_name, idem_key)，**不含调用主体**。
--   ai_tool_idempotency 的唯一约束是 uk_ai_tool_idempotency (tool_name, idem_key)，
--   于是两个不同用户用同一个 idempotencyKey 时，后者的写请求会命中前者的记录 ——
--   直接返回别人那次的结果，且**不真正执行写入**。两个后果都很重：
--     · 跨用户回放：B 拿到了 A 的写结果（含 result_text / result_data）；
--     · 静默丢写：B 以为写成功了，实际什么都没写。
--   idempotencyKey 是客户端生成的（MCP 客户端 / 模型自拟），撞键完全可能 ——
--   常见的就是 uuid 起点相同、或客户端固定用 "retry-1" 这类名字。
--
-- 修法（expand/contract 的 expand 半段）：
--   1. 加 actor 列（调用者 id；0 表示无主体的系统调用）；
--   2. 旧唯一约束换成 (tool_name, actor, idem_key)；
--   3. 存量行 actor 一律回填 0 —— 它们无法反推是谁写的，回填一个假的用户 id
--      会让"这条记录属于谁"变成假事实。0 的语义是"来源未知"，与新增的系统调用
--      共用同一档，安全方向正确：新用户的请求不会命中它们（除非他恰好也是 0）。
--
-- NOT NULL DEFAULT 0：加列时不需要单独回填语句（默认值即完成回填）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS、DROP CONSTRAINT IF EXISTS、约束/索引各自判定。

ALTER TABLE ai_tool_idempotency
    ADD COLUMN IF NOT EXISTS actor bigint NOT NULL DEFAULT 0;

-- 旧唯一约束必须先去掉：它挡着「同一 tool+key 在不同 actor 下各存一行」。
ALTER TABLE ai_tool_idempotency
    DROP CONSTRAINT IF EXISTS uk_ai_tool_idempotency;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('ai_tool_idempotency')
          AND conname = 'uk_ai_tool_idempotency_actor'
    ) THEN
        ALTER TABLE ai_tool_idempotency
            ADD CONSTRAINT uk_ai_tool_idempotency_actor UNIQUE (tool_name, actor, idem_key);
    END IF;
END $$;

COMMENT ON COLUMN ai_tool_idempotency.actor IS
    '调用者 id（B02 迁移 678）：幂等键的作用域是 (tool_name, actor, idem_key)。0 = 无主体的系统调用或迁移前的存量行';
