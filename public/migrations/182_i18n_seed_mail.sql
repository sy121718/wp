-- 182 · mail 模块文案词条（审计 I18N-002）。
--
-- 模块 enums 的常量值改为 i18n key 后，真正的文案落在这里。响应层
-- pkg/response.translate 按请求语言查表；未命中原样返回 key（可见的降级，不是空白）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('mail.msg.sendSuccess', 'zh-CN', '发送成功', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.sendSuccess', 'en-US', 'Sent', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.saveSuccess', 'zh-CN', '保存成功', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.saveSuccess', 'en-US', 'Saved', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.deleteSuccess', 'zh-CN', '删除成功', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.deleteSuccess', 'en-US', 'Deleted', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.importSuccess', 'zh-CN', '导入完成', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.importSuccess', 'en-US', 'Import complete', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.testSent', 'zh-CN', '测试邮件已发送', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.testSent', 'en-US', 'Test email sent', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.campaignStarted', 'zh-CN', '活动已开始发送', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.campaignStarted', 'en-US', 'Campaign started', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.automationStarted', 'zh-CN', '已加入流程', 'mail', '', 1, 200, now(), now()),
    ('mail.msg.automationStarted', 'en-US', 'Added to the flow', 'mail', '', 1, 200, now(), now()),
    ('mail.err.invalidParam', 'zh-CN', '参数不合法', 'mail', '', 1, 200, now(), now()),
    ('mail.err.invalidParam', 'en-US', 'Invalid parameter', 'mail', '', 1, 200, now(), now()),
    ('mail.err.accountNotFound', 'zh-CN', '发信账号不存在', 'mail', '', 1, 200, now(), now()),
    ('mail.err.accountNotFound', 'en-US', 'Sending account not found', 'mail', '', 1, 200, now(), now()),
    ('mail.err.templateNotFound', 'zh-CN', '邮件模板不存在', 'mail', '', 1, 200, now(), now()),
    ('mail.err.templateNotFound', 'en-US', 'Email template not found', 'mail', '', 1, 200, now(), now()),
    ('mail.err.contactNotFound', 'zh-CN', '联系人不存在', 'mail', '', 1, 200, now(), now()),
    ('mail.err.contactNotFound', 'en-US', 'Contact not found', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignNotFound', 'zh-CN', '活动不存在', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignNotFound', 'en-US', 'Campaign not found', 'mail', '', 1, 200, now(), now()),
    ('mail.err.accountDisabled', 'zh-CN', '发信账号已停用', 'mail', '', 1, 200, now(), now()),
    ('mail.err.accountDisabled', 'en-US', 'Sending account is disabled', 'mail', '', 1, 200, now(), now()),
    ('mail.err.accountIncomplete', 'zh-CN', '发信账号配置不完整（缺少主机 / 端口 / 发件人）', 'mail', '', 1, 200, now(), now()),
    ('mail.err.accountIncomplete', 'en-US', 'Sending account is incomplete (missing host / port / sender)', 'mail', '', 1, 200, now(), now()),
    ('mail.err.suppressed', 'zh-CN', '该地址在抑制名单中，不允许发送', 'mail', '', 1, 200, now(), now()),
    ('mail.err.suppressed', 'en-US', 'This address is on the suppression list and cannot be mailed', 'mail', '', 1, 200, now(), now()),
    ('mail.err.emailRequired', 'zh-CN', '邮箱不能为空', 'mail', '', 1, 200, now(), now()),
    ('mail.err.emailRequired', 'en-US', 'Email is required', 'mail', '', 1, 200, now(), now()),
    ('mail.err.emailInvalid', 'zh-CN', '邮箱格式不正确', 'mail', '', 1, 200, now(), now()),
    ('mail.err.emailInvalid', 'en-US', 'Invalid email address', 'mail', '', 1, 200, now(), now()),
    ('mail.err.importEmpty', 'zh-CN', '导入内容为空', 'mail', '', 1, 200, now(), now()),
    ('mail.err.importEmpty', 'en-US', 'Nothing to import', 'mail', '', 1, 200, now(), now()),
    ('mail.err.importTooLarge', 'zh-CN', '导入内容过大', 'mail', '', 1, 200, now(), now()),
    ('mail.err.importTooLarge', 'en-US', 'Import is too large', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignNotDraft', 'zh-CN', '只有草稿状态的活动可以修改', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignNotDraft', 'en-US', 'Only draft campaigns can be edited', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignNoRecipient', 'zh-CN', '投递目标为 0 人，无法发送', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignNoRecipient', 'en-US', 'No recipients; nothing to send', 'mail', '', 1, 200, now(), now()),
    ('mail.err.cipherSecretMissing', 'zh-CN', '未配置敏感数据加密密钥（config.yaml 的 app.secret），无法保存邮箱密码', 'mail', '', 1, 200, now(), now()),
    ('mail.err.cipherSecretMissing', 'en-US', 'Encryption key is not configured (config.yaml app.secret), cannot store the mailbox password', 'mail', '', 1, 200, now(), now()),
    ('mail.err.cipherUnavailable', 'zh-CN', '邮箱密码无法解密：加密密钥可能已变更，请重新填写密码', 'mail', '', 1, 200, now(), now()),
    ('mail.err.cipherUnavailable', 'en-US', 'Mailbox password cannot be decrypted; the key may have changed, please re-enter it', 'mail', '', 1, 200, now(), now()),
    ('mail.err.templateSyntax', 'zh-CN', '模板语法错误', 'mail', '', 1, 200, now(), now()),
    ('mail.err.templateSyntax', 'en-US', 'Template syntax error', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignSending', 'zh-CN', '活动正在发送中，无法删除', 'mail', '', 1, 200, now(), now()),
    ('mail.err.campaignSending', 'en-US', 'Campaign is sending and cannot be deleted', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationNotFound', 'zh-CN', '自动化流程不存在', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationNotFound', 'en-US', 'Automation flow not found', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationTriggerInvalid', 'zh-CN', '触发方式不合法', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationTriggerInvalid', 'en-US', 'Invalid trigger type', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationGraphInvalid', 'zh-CN', '流程定义不合法', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationGraphInvalid', 'en-US', 'Invalid flow definition', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationRunExists', 'zh-CN', '该联系人已在此流程中', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationRunExists', 'en-US', 'This contact is already in this flow', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationRunNotFound', 'zh-CN', '自动化实例不存在', 'mail', '', 1, 200, now(), now()),
    ('mail.err.automationRunNotFound', 'en-US', 'Automation run not found', 'mail', '', 1, 200, now(), now()),
    ('mail.test.mailSubject', 'zh-CN', 'go_wp 邮件配置测试', 'mail', '', 1, 200, now(), now()),
    ('mail.test.mailSubject', 'en-US', 'go_wp mail configuration test', 'mail', '', 1, 200, now(), now()),
    ('mail.test.mailText', 'zh-CN', '邮件配置连通性测试

如果你看到这封邮件，说明发信账号的配置可用：
- SMTP 连接与认证通过
- 中文主题编码正常
- 纯文本正文正常

本邮件由后台「测试发送」触发。', 'mail', '', 1, 200, now(), now()),
    ('mail.test.mailText', 'en-US', 'Mail configuration connectivity test

If you can read this email, the sending account is configured correctly:
- SMTP connection and authentication succeed
- The sender address is accepted by the server', 'mail', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
