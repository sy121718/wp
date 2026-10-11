-- 607: Comment review write UI defaults, two keys in both supported languages.
-- Missing pairs are backfilled; operator translations remain unchanged.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.comment.write.refresh', 'zh-CN', '刷新列表', 200, 'admin', 'Comment review refresh entry', 1, now(), now()),
('admin.comment.write.refresh', 'en-US', 'Refresh list', 200, 'admin', 'Comment review refresh entry', 1, now(), now()),
('admin.comment.write.noJs', 'zh-CN', '评论审核需要 JavaScript，请开启后重试。', 200, 'admin', 'Comment review noscript hint', 1, now(), now()),
('admin.comment.write.noJs', 'en-US', 'Comment review requires JavaScript. Enable it and try again.', 200, 'admin', 'Comment review noscript hint', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
