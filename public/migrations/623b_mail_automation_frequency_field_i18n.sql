-- 623b — 画布顶栏的频率 / 回信即退出控件词条（中英成对）。
--
-- 与 623（两列）同一个功能：那两列没有控件就配不了 —— 画布是流程的唯一编辑器。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据逐条枚举这 4 个 key（8 行）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.automation_canvas.field.frequency', 'zh-CN', '频率', 200, 'admin', 'admin.mail.automation_canvas.field.frequency', 1, now(), now()),
('admin.mail.automation_canvas.field.frequency', 'en-US', 'Frequency', 200, 'admin', 'admin.mail.automation_canvas.field.frequency', 1, now(), now()),
('admin.mail.automation_canvas.frequency.every_time', 'zh-CN', '每次触发都跑', 200, 'admin', 'admin.mail.automation_canvas.frequency.every_time', 1, now(), now()),
('admin.mail.automation_canvas.frequency.every_time', 'en-US', 'Run on every trigger', 200, 'admin', 'admin.mail.automation_canvas.frequency.every_time', 1, now(), now()),
('admin.mail.automation_canvas.frequency.once', 'zh-CN', '每人只跑一次', 200, 'admin', 'admin.mail.automation_canvas.frequency.once', 1, now(), now()),
('admin.mail.automation_canvas.frequency.once', 'en-US', 'Run at most once per contact', 200, 'admin', 'admin.mail.automation_canvas.frequency.once', 1, now(), now()),
('admin.mail.automation_canvas.field.exit_on_reply', 'zh-CN', '回信即退出', 200, 'admin', 'admin.mail.automation_canvas.field.exit_on_reply', 1, now(), now()),
('admin.mail.automation_canvas.field.exit_on_reply', 'en-US', 'Exit when the contact replies', 200, 'admin', 'admin.mail.automation_canvas.field.exit_on_reply', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
