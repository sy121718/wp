-- 623a — 「回信即退出」的运行文案（中英成对）。
--
-- 只在一条路径上产生：exit_on_reply=true 的流程收到回信时，实例被置 stopped 并写
-- error_message（运行文案编码，出口按请求语言还原）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_mail_automation_frequency_i18n.go 里逐条枚举这 1 个 key（2 行）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('mail.run.stoppedByReply', 'zh-CN', '联系人已回信，流程退出', 200, 'mail', 'mail.run.stoppedByReply', 1, now(), now()),
('mail.run.stoppedByReply', 'en-US', 'The contact replied, so the flow exited', 200, 'mail', 'mail.run.stoppedByReply', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
