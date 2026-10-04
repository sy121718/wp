-- 542 · ai_access_token：对外访问令牌（PAT）—— 让外部 harness / AI 以某个管理员身份调用本站工具。
--
-- 背景：工具层（77/78/98 批）已能让**站内**会话调用工具，权限走 Casbin（身份 = 当前登录管理员）。
--   外部调用没有会话，需要一个**可长期存在、可独立撤销、可限定范围**的身份凭证。
--   本表就是它：一次调用 = 一个令牌 + 一组权限点。
--
-- 存什么、不存什么（安全相关，改动前先读这里）：
--   · **不存明文**。明文只在创建时返回一次，此后库里只有 token_hash（SHA-256）。
--     这意味着「忘了就重新生成」是设计行为，不是缺陷。
--   · token_prefix 是明文的前若干位，**只用于展示与排错**（「我撤的是哪一把」），
--     它本身不足以还原令牌，但足以在列表里区分同名令牌。
--   · scopes 是**权限点子集**（`ai:...` 这类字符串的 JSON 数组），不是角色名：
--     权限点由 `internal/permission/codes.go` 定义，令牌能做什么 = 这组权限点 ∩
--     归属账号本身仍拥有的权限（两者都要过，见 VerifyToken 的调用方）。
--
-- 为什么归属 user_id 而不是「独立主体」：外部调用要能回答「谁批的、谁的账号」。
--   令牌不做匿名主体，越权排查时顺着 user_id 就能找到人；账号被停用时令牌一并失效
--   （校验时的活体判定在 service 层，不靠这张表的状态位）。
--
-- 撤销：status 置 0（不做物理删除）。已撤销的令牌留在表里，审计才能回答「这把我什么时候撤的」。
-- revoked_time 与 status 一起写（撤销时刻是运维要看的，不能只靠 update_time 反推）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / 独立 COMMENT ON，
--   重复执行安全；判据见 register_ai_access_token.go（只判本批这一张表在不在）。

CREATE TABLE IF NOT EXISTS ai_access_token (
    id             BIGSERIAL    PRIMARY KEY,
    name           VARCHAR(80)  NOT NULL DEFAULT '',
    user_id        BIGINT       NOT NULL DEFAULT 0,
    token_prefix   VARCHAR(16)  NOT NULL DEFAULT '',
    token_hash     VARCHAR(64)  NOT NULL DEFAULT '',
    scopes         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    status         SMALLINT     NOT NULL DEFAULT 1,
    expires_at     TIMESTAMPTZ,
    last_used_time TIMESTAMPTZ,
    revoked_time   TIMESTAMPTZ,
    create_time    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    update_time    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE ai_access_token IS '对外访问令牌（PAT）：外部 harness / AI 以某个管理员身份调用本站工具';
COMMENT ON COLUMN ai_access_token.name IS '用途备注（人在列表里认它）；同名允许，靠 id 与前缀区分';
COMMENT ON COLUMN ai_access_token.user_id IS '归属的后台账号 id：权限判定与审计都顺着它找人；0 = 未记录（历史数据）';
COMMENT ON COLUMN ai_access_token.token_prefix IS '明文前若干位，**仅供展示与排错**（不足以还原令牌）';
COMMENT ON COLUMN ai_access_token.token_hash IS '明文的 SHA-256；明文不落库，忘了只能重新生成';
COMMENT ON COLUMN ai_access_token.scopes IS '权限点子集（JSON 数组，如 ["order:list"]）；实际可用 = 本集合 ∩ 归属账号仍拥有的权限';
COMMENT ON COLUMN ai_access_token.status IS '1 = 启用，0 = 已撤销（撤销不删行，审计要能回答「什么时候撤的」）';
COMMENT ON COLUMN ai_access_token.expires_at IS '过期时刻；NULL = 不过期（由使用方按需设置）';
COMMENT ON COLUMN ai_access_token.last_used_time IS '最近一次成功使用时刻；NULL = 从未使用（列表里用来识别「发了没用」的令牌）';
COMMENT ON COLUMN ai_access_token.revoked_time IS '撤销时刻；与 status 一起写';

-- 校验路径是热路径（每次外部调用一次）：按前缀缩小候选，再比对哈希。
CREATE INDEX IF NOT EXISTS idx_ai_access_token_prefix ON ai_access_token (token_prefix);
-- 哈希唯一：同一明文不可能出现两行（重复生成的概率可忽略，撞上要报错而不是静默共存）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_access_token_hash ON ai_access_token (token_hash);
-- 列表按账号查。
CREATE INDEX IF NOT EXISTS idx_ai_access_token_user ON ai_access_token (user_id, id DESC);
