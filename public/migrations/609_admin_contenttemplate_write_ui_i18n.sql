-- 609: Content template JSON write messages.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.content.templates.bulk.noneSelected', 'zh-CN', '请先选择内容模板。', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.noneSelected', 'en-US', 'Select content templates first.', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.allDeleted', 'zh-CN', '已删除 {deleted} 个模板。', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.allDeleted', 'en-US', 'Deleted {deleted} templates.', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.allSkipped', 'zh-CN', '跳过 {skipped} 个模板。', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.allSkipped', 'en-US', 'Skipped {skipped} templates.', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.partial', 'zh-CN', '已删除 {deleted} 个，跳过 {skipped} 个模板。', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.partial', 'en-US', 'Deleted {deleted} templates; skipped {skipped}.', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.refresh', 'zh-CN', '刷新列表', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.bulk.refresh', 'en-US', 'Refresh list', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.write.noJs', 'zh-CN', '启用和删除内容模板需要 JavaScript。', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.content.templates.write.noJs', 'en-US', 'Enable JavaScript to activate or delete content templates.', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.common.write.failed', 'zh-CN', '操作失败，请稍后重试。', 200, 'admin', 'Content template JSON write', 1, now(), now()),
('admin.common.write.failed', 'en-US', 'Operation failed. Please try again.', 200, 'admin', 'Content template JSON write', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
