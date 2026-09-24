-- Reason status batch receipts: the old bulk keys describe deletion, not updates.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.bulk.reasonPartial', 'zh-CN', '已更新 %s 个原因，%s 个未能更新', 200, 'inventory', 'inventory reason batch status', 1, now(), now()),
('admin.inventory.bulk.reasonPartial', 'en-US', 'Updated %s reasons; %s could not be updated', 200, 'inventory', 'inventory reason batch status', 1, now(), now()),
('admin.inventory.bulk.reasonDone', 'zh-CN', '已更新 %s 个原因', 200, 'inventory', 'inventory reason batch status', 1, now(), now()),
('admin.inventory.bulk.reasonDone', 'en-US', 'Updated %s reasons', 200, 'inventory', 'inventory reason batch status', 1, now(), now()),
('admin.inventory.bulk.reasonNoneSelected', 'zh-CN', '请选择要操作的原因', 200, 'inventory', 'inventory reason batch status', 1, now(), now()),
('admin.inventory.bulk.reasonNoneSelected', 'en-US', 'Select reasons to update', 200, 'inventory', 'inventory reason batch status', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
