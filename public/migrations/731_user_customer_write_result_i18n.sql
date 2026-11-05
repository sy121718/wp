-- 客户写操作改为 Jet HTML 结果片段：补齐这些短词条的中英两行。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('custNoneSel', 'zh-CN', '请先选择客户。', 400, 'user', 'admin/user/customer_bulk_status_result.html', 1, now(), now()),
('custNoneSel', 'en-US', 'Select customers first.', 400, 'user', 'admin/user/customer_bulk_status_result.html', 1, now(), now()),
('custBulkStat', 'zh-CN', '已更新 {done} 个客户，{skipped} 个未处理。', 200, 'user', 'admin/user/customer_bulk_status_result.html', 1, now(), now()),
('custBulkStat', 'en-US', 'Updated {done} customers; {skipped} were not processed.', 200, 'user', 'admin/user/customer_bulk_status_result.html', 1, now(), now()),
('custBulkUnlock', 'zh-CN', '已解除锁定 {unlocked} 个，已清零失败计数 {cleared} 个，{noop} 个本来就未锁定，{skipped} 个未处理。', 200, 'user', 'admin/user/customer_bulk_unlock_result.html', 1, now(), now()),
('custBulkUnlock', 'en-US', 'Unlocked {unlocked}, cleared failure counts for {cleared}, {noop} were not locked, {skipped} were not processed.', 200, 'user', 'admin/user/customer_bulk_unlock_result.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
