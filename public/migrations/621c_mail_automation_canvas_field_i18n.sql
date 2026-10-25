-- 621c — 画布顶栏新增的 4 个控件词条（中英成对）。
--
-- 背景：画布成为流程的唯一编辑器后，「流程名称 / 触发方式」必须能在画布上填
-- （保存结构会把它们一起提交，服务端对空名直接拒绝）。这批 key 只服务这一件事。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_mail_automation_canvas_field_i18n.go 里**逐条枚举本批 5 个 item_key**
--（5 × 2 = 10 行），不用 LIKE 前缀、也不用全库总量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.automation_canvas.field.name', 'zh-CN', '流程名称', 200, 'admin', 'admin.mail.automation_canvas.field.name', 1, now(), now()),
('admin.mail.automation_canvas.field.name', 'en-US', 'Flow name', 200, 'admin', 'admin.mail.automation_canvas.field.name', 1, now(), now()),
('admin.mail.automation_canvas.field.name_ph', 'zh-CN', '例如：新订阅欢迎', 200, 'admin', 'admin.mail.automation_canvas.field.name_ph', 1, now(), now()),
('admin.mail.automation_canvas.field.name_ph', 'en-US', 'e.g. New subscriber welcome', 200, 'admin', 'admin.mail.automation_canvas.field.name_ph', 1, now(), now()),
('admin.mail.automation_canvas.field.trigger', 'zh-CN', '触发方式', 200, 'admin', 'admin.mail.automation_canvas.field.trigger', 1, now(), now()),
('admin.mail.automation_canvas.field.trigger', 'en-US', 'Trigger', 200, 'admin', 'admin.mail.automation_canvas.field.trigger', 1, now(), now()),
('admin.mail.automation_canvas.badge.new', 'zh-CN', '新流程', 200, 'admin', 'admin.mail.automation_canvas.badge.new', 1, now(), now()),
('admin.mail.automation_canvas.badge.new', 'en-US', 'New flow', 200, 'admin', 'admin.mail.automation_canvas.badge.new', 1, now(), now()),
('admin.mail.automation_canvas.side.save_hint', 'zh-CN', '改完参数后按顶栏的「保存结构」提交整张图。', 200, 'admin', 'admin.mail.automation_canvas.side.save_hint', 1, now(), now()),
('admin.mail.automation_canvas.side.save_hint', 'en-US', 'After editing, use Save structure in the top bar to submit the whole graph.', 200, 'admin', 'admin.mail.automation_canvas.side.save_hint', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
