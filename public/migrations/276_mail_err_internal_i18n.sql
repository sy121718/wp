-- 276 · mail 页面错误归口文案（审计 CQ-009 / 第三波收口）。
--
-- 背景：mail 模块的四个后台页面 handler 此前一律把 err.Error() 拼进 ?err= 或塞进渲染数据。
-- service 的业务错误**本身就是 i18n key**（mailenums.ErrAccountNotFound =
-- "mail.err.accountNotFound"），页面直接拼出来的是裸 key（运营看到「mail.err.campaignNotDraft」
-- 而不是「活动不在草稿状态」）；而基础设施错误的原文（PostgreSQL 表名 / 约束名 / SQLSTATE、
-- SMTP 主机与响应码）会一起直出到页面上 —— 响应不是可信边界。
-- 现在统一走 internal/module/mail/inbound/http/mail_err.go 的助手：
-- 命中本模块 enums 白名单（MailFacingMessages）→ 按 key 翻成当前语言；否则记结构化日志
--（带场景 / user_id / 路径）并返回本词条作为对外归口文案。详情只留在日志里。
--
-- 常量值即 key（internal/module/mail/enums/mail_enums.go 的 ErrInternal）。
-- 值带模块前缀是刻意的：sys_i18n 主键是 (item_key, lang)，裸 key "ErrInternal" 已被
-- admin 批占用（迁移 268）；navigation 同理（迁移 269）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('mail.err.internal', 'zh-CN', '操作失败，请稍后重试', 500, 'error', 'internal/module/mail/enums/mail_enums.go', 1, now(), now()),
    ('mail.err.internal', 'en-US', 'Operation failed, please try again later', 500, 'error', 'internal/module/mail/enums/mail_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
