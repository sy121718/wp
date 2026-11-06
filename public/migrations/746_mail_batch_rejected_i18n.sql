-- 邮件批量写统一整批拒绝文案：主消息只说「整批未执行」，
-- 逐条 id / 名称 / 原因由共享出口放进 data.items，不在主消息里拼明细。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('mailBatchReject', 'zh-CN', '整批未执行：有条目不能处理，请按明细修正后重试'),
('mailBatchReject', 'en-US', 'The batch was not executed. Correct the items that cannot be processed and try again.'),
('mailCsvBad', 'zh-CN', 'CSV 内容无法解析，请检查格式后重试'),
('mailCsvBad', 'en-US', 'The CSV content could not be parsed. Check the format and try again.')
ON CONFLICT (item_key, lang) DO NOTHING;
