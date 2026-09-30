-- 477 · 详情页预览的「1:1 打开」入口文案。
--
-- 背景：右栏的预览是**缩略**（iframe 按 1440 宽渲染再等比缩小）—— 缩略能看清完整形态，
--   但看不清细节（字号、间距、单行文案）。补一个 1:1 入口：新标签打开同一个预览帧，
--   用浏览器自己的窗口看细节，不需要在后台里再做一个「大预览」组件。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 2 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.article.edit.previewOpenFull', 'zh-CN', '新标签打开（1:1）', 200, 'admin', '详情页预览：1:1 入口', 1, now(), now()),
('admin.article.edit.previewOpenFull', 'en-US', 'Open in new tab (1:1)', 200, 'admin', '详情页预览：1:1 入口', 1, now(), now()),
('admin.products.preview.openFull', 'zh-CN', '新标签打开（1:1）', 200, 'admin', '详情页预览：1:1 入口', 1, now(), now()),
('admin.products.preview.openFull', 'en-US', 'Open in new tab (1:1)', 200, 'admin', '详情页预览：1:1 入口', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
