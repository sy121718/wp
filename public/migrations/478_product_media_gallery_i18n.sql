-- 478 · 商品图集（多图控件）的界面文案。
--
-- 背景：图集从「一行一个地址」的 textarea 换成多图控件（缩略图网格 + 媒体库多选 + 逐张 alt）。
--   这批是它自己的三句：添加按钮、alt 占位、空态（空态那句要顺带说清「上传也在媒体库里做」——
--   控件不持有文件输入，这句话是用户唯一会看到的入口说明）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 3 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.media.add', 'zh-CN', '添加图片', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('admin.products.media.add', 'en-US', 'Add images', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('admin.products.media.altPlaceholder', 'zh-CN', '这张图的 alt 文本', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('admin.products.media.altPlaceholder', 'en-US', 'Alt text for this image', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('admin.products.media.empty', 'zh-CN', '还没有图片。点「添加图片」从媒体库里挑 —— 上传也在媒体库里做。', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('admin.products.media.empty', 'en-US', 'No images yet. Use "Add images" to pick from the media library — uploads happen there too.', 200, 'admin', '商品图集多图控件', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
