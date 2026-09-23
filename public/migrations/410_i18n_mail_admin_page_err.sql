-- ========================================
-- 410 — 邮件公开链接（点击追踪 / 一键退订）的访客面词条
--
-- 背景：mail 模块的三个访客端点（GET /_t/o/{token}.gif、/_t/c/{token}、/_t/u/{token}）
-- 是**邮件里的链接**：收件人在邮件客户端点开，浏览器直接打开 —— 无登录态、无后台页面壳。
-- 失败时给一句受控短句、成功时给一张自带样式的整页 HTML，这个形态是对的
-- （访客没有可回归的列表页，也没有解析 JSON 的客户端；改成 303 + ?err= 或 JSON 都无处可去）。
-- 缺的是**文案来源**：三处文案此前是 Go 里的硬编码中文，而收件人可能是英文用户
-- （Accept-Language: en 的浏览器打开中文页面），且中文文案连词条都没有 ——
-- 运营想改措辞也改不了（改的是代码）。
--
-- 本批改为按请求语言取词条（internal/module/mail/inbound/http/mail_err.go 的 mailVisitorText，
-- 语言来自 response.RequestLanguage 的协商链：Cookie lang → query lang → Accept-Language → 默认语言，
-- **不需要登录态**，裸引擎路由直接用得上）。zh-CN 行的值与 Go 侧的中文兜底常量逐字一致：
-- 兜底是给「词条缺失 / i18n 未初始化」用的，两句不一致会让同一个页面按环境说不同的话。
--
-- 迁移名里的 admin 是批次名（同批的 admin 域 dev_login 四处出口**判定不改**，理由见批次报告：
-- release 不注册该路由、消费者是开发者与自动化脚本、文案已受控且明确指向服务端日志 ——
-- 改动收益低于后门形状端点的回归风险）。故本文件只含 mail 的 5 个 key。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批不修改任何既有词条的值。
-- 占位符协议：mail.msg.unsubscribeDoneBody 的 %s 是收件人邮箱，在 Go 侧用 strings.ReplaceAll
-- 替换（不走 Sprintf，避免词条里混进协议外占位符时把 Go 的格式错误输出摆给收件人）。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('mail.err.trackLinkInvalid', 'zh-CN', '链接无效或已过期', 400, 'error', 'mail/inbound/http: 点击追踪端点 /_t/c/{token} 验签失败或链接过期（访客面短句）', 1, now(), now()),
('mail.err.trackLinkInvalid', 'en-US', 'This link is invalid or has expired', 400, 'error', 'mail/inbound/http: 点击追踪端点 /_t/c/{token} 验签失败或链接过期（访客面短句）', 1, now(), now()),
('mail.err.unsubscribeLinkInvalid', 'zh-CN', '退订链接无效或已过期', 400, 'error', 'mail/inbound/http: 退订端点 /_t/u/{token} 失败（token 无效 / 联系人查不到 / 写库失败，原文只进日志）', 1, now(), now()),
('mail.err.unsubscribeLinkInvalid', 'en-US', 'This unsubscribe link is invalid or has expired', 400, 'error', 'mail/inbound/http: 退订端点 /_t/u/{token} 失败（token 无效 / 联系人查不到 / 写库失败，原文只进日志）', 1, now(), now()),
('mail.msg.unsubscribeDoneTitle', 'zh-CN', '已退订', 200, 'mail', 'mail/inbound/http: 退订成功页标题（<title> 与 <h2> 共用）', 1, now(), now()),
('mail.msg.unsubscribeDoneTitle', 'en-US', 'Unsubscribed', 200, 'mail', 'mail/inbound/http: 退订成功页标题（<title> 与 <h2> 共用）', 1, now(), now()),
('mail.msg.unsubscribeDoneBody', 'zh-CN', '%s 不会再收到我们的营销邮件。', 200, 'mail', 'mail/inbound/http: 退订成功页正文，%s = 收件人邮箱（Go 侧 ReplaceAll 注入）', 1, now(), now()),
('mail.msg.unsubscribeDoneBody', 'en-US', '%s will no longer receive our marketing emails.', 200, 'mail', 'mail/inbound/http: 退订成功页正文，%s = 收件人邮箱（Go 侧 ReplaceAll 注入）', 1, now(), now()),
('mail.msg.unsubscribeDoneNote', 'zh-CN', '事务类邮件（如密码重置、订单通知）不受影响。', 200, 'mail', 'mail/inbound/http: 退订成功页补充说明（退订只影响营销邮件）', 1, now(), now()),
('mail.msg.unsubscribeDoneNote', 'en-US', 'Transactional emails (such as password resets and order notifications) are not affected.', 200, 'mail', 'mail/inbound/http: 退订成功页补充说明（退订只影响营销邮件）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
