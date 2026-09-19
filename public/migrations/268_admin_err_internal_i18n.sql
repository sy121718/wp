-- 268 · admin 内部错误归口文案（审计 CQ-009/CQ-010 的 admin 收口）。
--
-- 背景：admin 的 inbound/http 里有 74 处把 err.Error() 拼进响应消息
-- （44 处 `MsgBadRequest+": "+err.Error()` 与 30 处 `err.Error()`），
-- 其中包含 PostgreSQL 原文 —— 表名、唯一约束名（uq_sys_role_role_code）、SQLSTATE。
-- 现在统一走 internal/module/admin/inbound/http/admin_err.go 的助手：
-- 命中本模块 enums 业务文案（白名单）→ 原样返回；否则记结构化日志（带场景与 user_id）
-- 并返回本词条作为对外归口文案。细节只留在日志里，不再出现在响应中。
--
-- 常量值即 key（internal/module/admin/enums/admin_enums.go 的 ErrInternal）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('ErrInternal', 'zh-CN', '操作失败，请稍后重试', 500, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('ErrInternal', 'en-US', 'Operation failed, please try again later', 500, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
