-- 621e — T4 的四个新节点：校验明细 + 运行文案（中英成对）。
--
-- 两组：
--   · admin.mail.detail.*  六条：filter / list_status_branch / ab_test / webhook 的参数校验明细
--     （分别由 nodeArity 的 requireAllOutlets / checkABTest / webhook 分支产出）；
--   · mail.run.*  七条：执行期写进节点日志的运行文案（卡状态分流 / 命中变体 / 缺出口 /
--     回调成败与拒绝原因）。落库的是 key + 参数编码，出口按请求语言还原。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_mail_automation_nodes_i18n.go 里**逐条枚举本批 13 个 item_key**（26 行）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— 校验明细（画布保存时回显） ——
('admin.mail.detail.outletMissing', 'zh-CN', '节点 {node}: 缺少出口 {outlet}', 200, 'admin', 'admin.mail.detail.outletMissing', 1, now(), now()),
('admin.mail.detail.outletMissing', 'en-US', 'Node {node}: outlet(s) {outlet} are not connected.', 200, 'admin', 'admin.mail.detail.outletMissing', 1, now(), now()),
('admin.mail.detail.needVariants', 'zh-CN', '节点 {node}: 分流转需要 2–5 个变体', 200, 'admin', 'admin.mail.detail.needVariants', 1, now(), now()),
('admin.mail.detail.needVariants', 'en-US', 'Node {node}: an A/B split needs 2 to 5 variants.', 200, 'admin', 'admin.mail.detail.needVariants', 1, now(), now()),
('admin.mail.detail.variantDuplicate', 'zh-CN', '节点 {node}: 变体 id 重复: {variant}', 200, 'admin', 'admin.mail.detail.variantDuplicate', 1, now(), now()),
('admin.mail.detail.variantDuplicate', 'en-US', 'Node {node}: duplicate variant id: {variant}', 200, 'admin', 'admin.mail.detail.variantDuplicate', 1, now(), now()),
('admin.mail.detail.variantWeightBad', 'zh-CN', '节点 {node}: 变体权重之和必须是 100（当前 {weight}）', 200, 'admin', 'admin.mail.detail.variantWeightBad', 1, now(), now()),
('admin.mail.detail.variantWeightBad', 'en-US', 'Node {node}: variant weights must add up to 100 (currently {weight}).', 200, 'admin', 'admin.mail.detail.variantWeightBad', 1, now(), now()),
('admin.mail.detail.needWebhookURL', 'zh-CN', '节点 {node}: 回调节点需要 https 地址', 200, 'admin', 'admin.mail.detail.needWebhookURL', 1, now(), now()),
('admin.mail.detail.needWebhookURL', 'en-US', 'Node {node}: a webhook node needs an https URL.', 200, 'admin', 'admin.mail.detail.needWebhookURL', 1, now(), now()),
('admin.mail.detail.webhookDenied', 'zh-CN', '节点 {node}: 回调地址不允许指向内网', 200, 'admin', 'admin.mail.detail.webhookDenied', 1, now(), now()),
('admin.mail.detail.webhookDenied', 'en-US', 'Node {node}: the webhook URL must not point at a private network.', 200, 'admin', 'admin.mail.detail.webhookDenied', 1, now(), now()),
-- —— 运行文案（写进节点日志） ——
('mail.run.branchOut', 'zh-CN', '按状态走 {outlet} 分支', 200, 'mail', 'mail.run.branchOut', 1, now(), now()),
('mail.run.branchOut', 'en-US', 'Taking the {outlet} branch by contact status', 200, 'mail', 'mail.run.branchOut', 1, now(), now()),
('mail.run.abVariant', 'zh-CN', '命中变体 {variant}', 200, 'mail', 'mail.run.abVariant', 1, now(), now()),
('mail.run.abVariant', 'en-US', 'Assigned to variant {variant}', 200, 'mail', 'mail.run.abVariant', 1, now(), now()),
('mail.run.outletMissing', 'zh-CN', '节点 {node} 缺少出口 {outlet}', 200, 'mail', 'mail.run.outletMissing', 1, now(), now()),
('mail.run.outletMissing', 'en-US', 'Node {node} has no connection for outlet {outlet}', 200, 'mail', 'mail.run.outletMissing', 1, now(), now()),
('mail.run.webhookOk', 'zh-CN', '回调已发出（HTTP {status}）', 200, 'mail', 'mail.run.webhookOk', 1, now(), now()),
('mail.run.webhookOk', 'en-US', 'Webhook delivered (HTTP {status})', 200, 'mail', 'mail.run.webhookOk', 1, now(), now()),
('mail.run.webhookFailed', 'zh-CN', '回调失败: {reason}（流程继续）', 200, 'mail', 'mail.run.webhookFailed', 1, now(), now()),
('mail.run.webhookFailed', 'en-US', 'Webhook failed: {reason} (the flow continues)', 200, 'mail', 'mail.run.webhookFailed', 1, now(), now()),
('mail.run.webhookNoUrl', 'zh-CN', '回调节点没有配置地址', 200, 'mail', 'mail.run.webhookNoUrl', 1, now(), now()),
('mail.run.webhookNoUrl', 'en-US', 'The webhook node has no URL configured', 200, 'mail', 'mail.run.webhookNoUrl', 1, now(), now()),
('mail.run.webhookDenied', 'zh-CN', '回调地址指向内网，已拒绝', 200, 'mail', 'mail.run.webhookDenied', 1, now(), now()),
('mail.run.webhookDenied', 'en-US', 'The webhook URL points at a private network and was rejected', 200, 'mail', 'mail.run.webhookDenied', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
