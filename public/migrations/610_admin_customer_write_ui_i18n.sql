-- 610: Customer JSON write messages.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customers.write.noneSelected', 'zh-CN', '请先选择客户。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.noneSelected', 'en-US', 'Select customers first.', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.allUpdated', 'zh-CN', '已更新 {updated} 个客户。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.allUpdated', 'en-US', 'Updated {updated} customers.', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.allSkipped', 'zh-CN', '{skipped} 个客户未处理。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.allSkipped', 'en-US', 'Skipped {skipped} customers.', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.partial', 'zh-CN', '已更新 {updated} 个，{skipped} 个未处理。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.partial', 'en-US', 'Updated {updated} customers; skipped {skipped}.', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.refresh', 'zh-CN', '刷新列表', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.refresh', 'en-US', 'Refresh list', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.failed', 'zh-CN', '操作失败，请稍后重试。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.failed', 'en-US', 'Operation failed. Please try again.', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.noJs', 'zh-CN', '修改客户状态和解除锁定需要 JavaScript。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('admin.customers.write.noJs', 'en-US', 'Enable JavaScript to change customer status or unlock accounts.', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('user.msg.customerBulkUnlock', 'zh-CN', '已解除锁定 {unlocked} 个，已清零失败计数 {cleared} 个，{noop} 个本来就未锁定，{skipped} 个未处理。', 200, 'admin', 'Customer JSON write', 1, now(), now()),
('user.msg.customerBulkUnlock', 'en-US', 'Unlocked {unlocked} accounts, cleared failure counts for {cleared}, {noop} already unlocked, {skipped} skipped.', 200, 'admin', 'Customer JSON write', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
