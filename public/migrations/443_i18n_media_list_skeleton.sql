-- The media list page gained a standard list skeleton (page head / filter bar /
-- bulk bar). All new UI strings are seeded in both languages; the entries are
-- symmetric, so the zh/en key sets stay aligned.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.media.filter.label', 'zh-CN', '文件名', 200, 'admin', 'admin/media/media.html: search field label', 1, now(), now()),
('admin.media.filter.label', 'en-US', 'File name', 200, 'admin', 'admin/media/media.html: search field label', 1, now(), now()),
('admin.media.list.title', 'zh-CN', '文件列表', 200, 'admin', 'admin/media/media.html: file list toolbar title', 1, now(), now()),
('admin.media.list.title', 'en-US', 'Files', 200, 'admin', 'admin/media/media.html: file list toolbar title', 1, now(), now()),
('admin.media.list.aria', 'zh-CN', '媒体列表', 200, 'admin', 'admin/media/media.html: table region aria label', 1, now(), now()),
('admin.media.list.aria', 'en-US', 'Media list', 200, 'admin', 'admin/media/media.html: table region aria label', 1, now(), now()),
('admin.media.bulk.selected', 'zh-CN', '已选 0 项', 200, 'admin', 'admin/media/media.html: bulk bar initial count', 1, now(), now()),
('admin.media.bulk.selected', 'en-US', '0 selected', 200, 'admin', 'admin/media/media.html: bulk bar initial count', 1, now(), now()),
('admin.media.bulk.download_scope', 'zh-CN', '下载选中附件的资源包', 200, 'admin', 'admin/media/media.html: batch download title (covers non-image local files)', 1, now(), now()),
('admin.media.bulk.download_scope', 'en-US', 'Download selected attachments as a zip', 200, 'admin', 'admin/media/media.html: batch download title (covers non-image local files)', 1, now(), now()),
('admin.media.bulk.delete', 'zh-CN', '批量删除', 200, 'admin', 'admin/media/media.html: batch delete button', 1, now(), now()),
('admin.media.bulk.delete', 'en-US', 'Delete selected', 200, 'admin', 'admin/media/media.html: batch delete button', 1, now(), now()),
('admin.media.bulk.select_all', 'zh-CN', '全选当前页', 200, 'admin', 'admin/media/media.html: grid select-all label', 1, now(), now()),
('admin.media.bulk.select_all', 'en-US', 'Select all on this page', 200, 'admin', 'admin/media/media.html: grid select-all label', 1, now(), now()),
('admin.media.bulk.select_all_aria', 'zh-CN', '全选当前页附件', 200, 'admin', 'admin/media/media.html: select-all checkbox aria label', 1, now(), now()),
('admin.media.bulk.select_all_aria', 'en-US', 'Select all attachments on this page', 200, 'admin', 'admin/media/media.html: select-all checkbox aria label', 1, now(), now()),
('admin.media.col.actions', 'zh-CN', '操作', 200, 'admin', 'admin/media/media.html: row actions column header', 1, now(), now()),
('admin.media.col.actions', 'en-US', 'Actions', 200, 'admin', 'admin/media/media.html: row actions column header', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
