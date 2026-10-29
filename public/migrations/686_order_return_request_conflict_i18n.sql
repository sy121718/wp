-- 订单售后幂等键冲突的错误事实，中英成对。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('order.err.returnRequestConflict', 'zh-CN', '这个退货请求已用于另一份申请，请重新发起'),
('order.err.returnRequestConflict', 'en-US', 'This return request has already been used for another application. Please start a new request.')
ON CONFLICT (item_key, lang) DO NOTHING;
