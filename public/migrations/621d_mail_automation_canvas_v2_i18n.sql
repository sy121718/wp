-- 621d — 画布重写（T3）新增的界面词条（中英成对）。
--
-- 两组：
--   · admin.mail.automation_node.*  —— 节点类型的名字与提示。旧的 automation_edit.node.*
--     是表单页的命名空间（那个页面已删），画布重写把类型表挪到自己的命名空间下：
--     十个类型一次到位（trigger / delay / email / filter / list_status_branch / ab_test /
--     webhook / tag / end），palette、出口菜单、节点盒上的名字全部读这一份。
--   · admin.mail.automation_canvas.*  —— 画布新控件的文案（撤销/重做/自动布局/缩略图/
--     palette 标题/删除节点/键盘提示/发信节点被禁用的原因）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_mail_automation_canvas_v2_i18n.go 里**逐条枚举本批 30 个 item_key**
--（30 × 2 = 60 行），不用 LIKE 前缀、也不用全库总量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— 节点类型：入口 ——
('admin.mail.automation_node.trigger.label', 'zh-CN', '入口', 200, 'admin', 'admin.mail.automation_node.trigger.label', 1, now(), now()),
('admin.mail.automation_node.trigger.label', 'en-US', 'Entry', 200, 'admin', 'admin.mail.automation_node.trigger.label', 1, now(), now()),
('admin.mail.automation_node.trigger.hint', 'zh-CN', '流程从这里开始，不需要参数', 200, 'admin', 'admin.mail.automation_node.trigger.hint', 1, now(), now()),
('admin.mail.automation_node.trigger.hint', 'en-US', 'The flow starts here; no parameters.', 200, 'admin', 'admin.mail.automation_node.trigger.hint', 1, now(), now()),
-- —— 等待 ——
('admin.mail.automation_node.delay.label', 'zh-CN', '等待', 200, 'admin', 'admin.mail.automation_node.delay.label', 1, now(), now()),
('admin.mail.automation_node.delay.label', 'en-US', 'Wait', 200, 'admin', 'admin.mail.automation_node.delay.label', 1, now(), now()),
('admin.mail.automation_node.delay.hint', 'zh-CN', '等待若干分钟再继续', 200, 'admin', 'admin.mail.automation_node.delay.hint', 1, now(), now()),
('admin.mail.automation_node.delay.hint', 'en-US', 'Wait a number of minutes before continuing.', 200, 'admin', 'admin.mail.automation_node.delay.hint', 1, now(), now()),
-- —— 发邮件 ——
('admin.mail.automation_node.email.label', 'zh-CN', '发邮件', 200, 'admin', 'admin.mail.automation_node.email.label', 1, now(), now()),
('admin.mail.automation_node.email.label', 'en-US', 'Send email', 200, 'admin', 'admin.mail.automation_node.email.label', 1, now(), now()),
('admin.mail.automation_node.email.hint', 'zh-CN', '用某个邮件模板发一封信', 200, 'admin', 'admin.mail.automation_node.email.hint', 1, now(), now()),
('admin.mail.automation_node.email.hint', 'en-US', 'Send one email using a template.', 200, 'admin', 'admin.mail.automation_node.email.hint', 1, now(), now()),
-- —— 条件分支 ——
('admin.mail.automation_node.filter.label', 'zh-CN', '条件分支', 200, 'admin', 'admin.mail.automation_node.filter.label', 1, now(), now()),
('admin.mail.automation_node.filter.label', 'en-US', 'Condition', 200, 'admin', 'admin.mail.automation_node.filter.label', 1, now(), now()),
('admin.mail.automation_node.filter.hint', 'zh-CN', '条件满足走 yes，不满足走 no', 200, 'admin', 'admin.mail.automation_node.filter.hint', 1, now(), now()),
('admin.mail.automation_node.filter.hint', 'en-US', 'Take yes when the conditions hold, no otherwise.', 200, 'admin', 'admin.mail.automation_node.filter.hint', 1, now(), now()),
-- —— 按订阅状态分流 ——
('admin.mail.automation_node.list_status_branch.label', 'zh-CN', '按订阅状态分流', 200, 'admin', 'admin.mail.automation_node.list_status_branch.label', 1, now(), now()),
('admin.mail.automation_node.list_status_branch.label', 'en-US', 'Split by subscription status', 200, 'admin', 'admin.mail.automation_node.list_status_branch.label', 1, now(), now()),
('admin.mail.automation_node.list_status_branch.hint', 'zh-CN', '已订阅 / 待确认 / 不在名单，各走一条', 200, 'admin', 'admin.mail.automation_node.list_status_branch.hint', 1, now(), now()),
('admin.mail.automation_node.list_status_branch.hint', 'en-US', 'Subscribed, pending and not-in-list each take their own edge.', 200, 'admin', 'admin.mail.automation_node.list_status_branch.hint', 1, now(), now()),
-- —— A/B 分流 ——
('admin.mail.automation_node.ab_test.label', 'zh-CN', 'A/B 分流', 200, 'admin', 'admin.mail.automation_node.ab_test.label', 1, now(), now()),
('admin.mail.automation_node.ab_test.label', 'en-US', 'A/B split', 200, 'admin', 'admin.mail.automation_node.ab_test.label', 1, now(), now()),
('admin.mail.automation_node.ab_test.hint', 'zh-CN', '按权重把联系人分到不同变体（2–5 个，权重和 100）', 200, 'admin', 'admin.mail.automation_node.ab_test.hint', 1, now(), now()),
('admin.mail.automation_node.ab_test.hint', 'en-US', 'Assign contacts to variants by weight (2 to 5 variants, weights summing to 100).', 200, 'admin', 'admin.mail.automation_node.ab_test.hint', 1, now(), now()),
-- —— 回调 ——
('admin.mail.automation_node.webhook.label', 'zh-CN', '回调', 200, 'admin', 'admin.mail.automation_node.webhook.label', 1, now(), now()),
('admin.mail.automation_node.webhook.label', 'en-US', 'Webhook', 200, 'admin', 'admin.mail.automation_node.webhook.label', 1, now(), now()),
('admin.mail.automation_node.webhook.hint', 'zh-CN', '向一个 https 地址发 POST（内网地址会被拒绝）', 200, 'admin', 'admin.mail.automation_node.webhook.hint', 1, now(), now()),
('admin.mail.automation_node.webhook.hint', 'en-US', 'POST to an https URL (private addresses are rejected).', 200, 'admin', 'admin.mail.automation_node.webhook.hint', 1, now(), now()),
-- —— 打标签 ——
('admin.mail.automation_node.tag.label', 'zh-CN', '打标签', 200, 'admin', 'admin.mail.automation_node.tag.label', 1, now(), now()),
('admin.mail.automation_node.tag.label', 'en-US', 'Tag contact', 200, 'admin', 'admin.mail.automation_node.tag.label', 1, now(), now()),
('admin.mail.automation_node.tag.hint', 'zh-CN', '给联系人加标签或减标签', 200, 'admin', 'admin.mail.automation_node.tag.hint', 1, now(), now()),
('admin.mail.automation_node.tag.hint', 'en-US', 'Add or remove tags on the contact.', 200, 'admin', 'admin.mail.automation_node.tag.hint', 1, now(), now()),
-- —— 结束 ——
('admin.mail.automation_node.end.label', 'zh-CN', '结束', 200, 'admin', 'admin.mail.automation_node.end.label', 1, now(), now()),
('admin.mail.automation_node.end.label', 'en-US', 'End', 200, 'admin', 'admin.mail.automation_node.end.label', 1, now(), now()),
('admin.mail.automation_node.end.hint', 'zh-CN', '流程到此结束，不需要参数', 200, 'admin', 'admin.mail.automation_node.end.hint', 1, now(), now()),
('admin.mail.automation_node.end.hint', 'en-US', 'The flow ends here; no parameters.', 200, 'admin', 'admin.mail.automation_node.end.hint', 1, now(), now()),
-- —— 画布控件 ——
('admin.mail.automation_canvas.palette.head', 'zh-CN', '添加节点', 200, 'admin', 'admin.mail.automation_canvas.palette.head', 1, now(), now()),
('admin.mail.automation_canvas.palette.head', 'en-US', 'Add a node', 200, 'admin', 'admin.mail.automation_canvas.palette.head', 1, now(), now()),
('admin.mail.automation_canvas.palette.aria', 'zh-CN', '可添加的节点', 200, 'admin', 'admin.mail.automation_canvas.palette.aria', 1, now(), now()),
('admin.mail.automation_canvas.palette.aria', 'en-US', 'Nodes you can add', 200, 'admin', 'admin.mail.automation_canvas.palette.aria', 1, now(), now()),
('admin.mail.automation_canvas.undo', 'zh-CN', '撤销（Ctrl+Z）', 200, 'admin', 'admin.mail.automation_canvas.undo', 1, now(), now()),
('admin.mail.automation_canvas.undo', 'en-US', 'Undo (Ctrl+Z)', 200, 'admin', 'admin.mail.automation_canvas.undo', 1, now(), now()),
('admin.mail.automation_canvas.redo', 'zh-CN', '重做（Ctrl+Shift+Z）', 200, 'admin', 'admin.mail.automation_canvas.redo', 1, now(), now()),
('admin.mail.automation_canvas.redo', 'en-US', 'Redo (Ctrl+Shift+Z)', 200, 'admin', 'admin.mail.automation_canvas.redo', 1, now(), now()),
('admin.mail.automation_canvas.fit', 'zh-CN', '自动布局并回到左上角', 200, 'admin', 'admin.mail.automation_canvas.fit', 1, now(), now()),
('admin.mail.automation_canvas.fit', 'en-US', 'Auto-layout and scroll back to the top-left', 200, 'admin', 'admin.mail.automation_canvas.fit', 1, now(), now()),
('admin.mail.automation_canvas.fit_label', 'zh-CN', '自动布局', 200, 'admin', 'admin.mail.automation_canvas.fit_label', 1, now(), now()),
('admin.mail.automation_canvas.fit_label', 'en-US', 'Auto-layout', 200, 'admin', 'admin.mail.automation_canvas.fit_label', 1, now(), now()),
('admin.mail.automation_canvas.mini.title', 'zh-CN', '缩略图：点一下把视口移过去', 200, 'admin', 'admin.mail.automation_canvas.mini.title', 1, now(), now()),
('admin.mail.automation_canvas.mini.title', 'en-US', 'Overview: click to move the viewport there', 200, 'admin', 'admin.mail.automation_canvas.mini.title', 1, now(), now()),
('admin.mail.automation_canvas.delete_node', 'zh-CN', '删除这个节点', 200, 'admin', 'admin.mail.automation_canvas.delete_node', 1, now(), now()),
('admin.mail.automation_canvas.delete_node', 'en-US', 'Delete this node', 200, 'admin', 'admin.mail.automation_canvas.delete_node', 1, now(), now()),
('admin.mail.automation_canvas.kbd.undo', 'zh-CN', ' 撤销，', 200, 'admin', 'admin.mail.automation_canvas.kbd.undo', 1, now(), now()),
('admin.mail.automation_canvas.kbd.undo', 'en-US', ' undo, ', 200, 'admin', 'admin.mail.automation_canvas.kbd.undo', 1, now(), now()),
('admin.mail.automation_canvas.kbd.del', 'zh-CN', ' 删节点，', 200, 'admin', 'admin.mail.automation_canvas.kbd.del', 1, now(), now()),
('admin.mail.automation_canvas.kbd.del', 'en-US', ' deletes the selected node, ', 200, 'admin', 'admin.mail.automation_canvas.kbd.del', 1, now(), now()),
('admin.mail.automation_canvas.kbd.save', 'zh-CN', ' 保存。', 200, 'admin', 'admin.mail.automation_canvas.kbd.save', 1, now(), now()),
('admin.mail.automation_canvas.kbd.save', 'en-US', ' saves.', 200, 'admin', 'admin.mail.automation_canvas.kbd.save', 1, now(), now()),
('admin.mail.automation_canvas.email_disabled', 'zh-CN', '先在「发信账号」里配置一个营销用途的 SMTP，再回来加发信节点', 200, 'admin', 'admin.mail.automation_canvas.email_disabled', 1, now(), now()),
('admin.mail.automation_canvas.email_disabled', 'en-US', 'Configure a marketing SMTP account under Sending accounts first, then come back to add the email node.', 200, 'admin', 'admin.mail.automation_canvas.email_disabled', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
