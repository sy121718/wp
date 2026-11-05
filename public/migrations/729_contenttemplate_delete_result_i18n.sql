-- 内容模板删除结果完整句：成功计数由具名参数承载，不拆词条拼句。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ctplDeleteDone', 'zh-CN', '已删除 {done} 项，跳过 {skipped} 项。', 200, 'contenttemplate', 'admin/contenttemplate/content_template_delete_result.html', 1, now(), now()),
('ctplDeleteDone', 'en-US', 'Deleted {done}, skipped {skipped}.', 200, 'contenttemplate', 'admin/contenttemplate/content_template_delete_result.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
