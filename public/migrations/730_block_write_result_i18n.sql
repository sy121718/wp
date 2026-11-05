-- 全局块页面写操作改为 Jet HTML 结果片段：补齐这些短词条的中英两行。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('blkCreated', 'zh-CN', '块「{name}」已创建，正在进入编辑。', 200, 'block', 'admin/block/block_created.html', 1, now(), now()),
('blkCreated', 'en-US', 'Block "{name}" created; opening the editor.', 200, 'block', 'admin/block/block_created.html', 1, now(), now()),
('blkOpenEdit', 'zh-CN', '立即打开编辑', 200, 'block', 'admin/block/block_created.html', 1, now(), now()),
('blkOpenEdit', 'en-US', 'Open the editor now', 200, 'block', 'admin/block/block_created.html', 1, now(), now()),
('blkSaved', 'zh-CN', '块「{name}」已保存，相关页面已标记待重建。', 200, 'block', 'admin/block/block_content_saved.html', 1, now(), now()),
('blkSaved', 'en-US', 'Block "{name}" saved; related pages are marked for rebuild.', 200, 'block', 'admin/block/block_content_saved.html', 1, now(), now()),
('blkNoneSel', 'zh-CN', '没有选中任何块，列表未改动。', 400, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkNoneSel', 'en-US', 'No blocks selected; the list is unchanged.', 400, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkDelNone', 'zh-CN', '没有块被删除：{skipped} 个不存在或被页面引用。', 200, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkDelNone', 'en-US', 'No block was deleted: {skipped} were missing or still referenced.', 200, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkDelDone', 'zh-CN', '已删除 {count} 个块。', 200, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkDelDone', 'en-US', 'Deleted {count} blocks.', 200, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkDelPart', 'zh-CN', '已删除 {done} 个块，另有 {skipped} 个被跳过（不存在或被页面引用）。', 200, 'block', 'admin/block/block_bulk_result.html', 1, now(), now()),
('blkDelPart', 'en-US', 'Deleted {done} blocks; {skipped} more were skipped (missing or still referenced).', 200, 'block', 'admin/block/block_bulk_result.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
