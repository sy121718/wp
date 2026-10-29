-- 订单与退货批量写统一整批拒绝文案，中文与模块声明兜底一致。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('order.err.bulkRejected', 'zh-CN', '整批未执行：有条目不能处理，请按明细修正后重试'),
('order.err.bulkRejected', 'en-US', 'The batch was not executed. Correct the items that cannot be processed and try again.')
ON CONFLICT (item_key, lang) DO NOTHING;
