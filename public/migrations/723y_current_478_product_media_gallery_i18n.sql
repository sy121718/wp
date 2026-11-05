-- 当前词条回补源；历史原文 478_product_media_gallery_i18n.sql 保持归档，不再执行。
-- 478 · 商品图集（多图控件）的界面文案。
--
-- 背景：图集从「一行一个地址」的 textarea 换成多图控件（缩略图网格 + 媒体库多选 + 逐张 alt）。
--   这批是它自己的三句：添加按钮、alt 占位、空态（空态那句要顺带说清「上传也在媒体库里做」——
--   控件不持有文件输入，这句话是用户唯一会看到的入口说明）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 3 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pMediaAdd', 'zh-CN', '添加图片', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('pMediaAdd', 'en-US', 'Add images', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('pMediaAltPh', 'zh-CN', '这张图的 alt 文本', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('pMediaAltPh', 'en-US', 'Alt text for this image', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('pMediaEmpty', 'zh-CN', '还没有图片。点「添加图片」从媒体库里挑 —— 上传也在媒体库里做。', 200, 'admin', '商品图集多图控件', 1, now(), now()),
('pMediaEmpty', 'en-US', 'No images yet. Use "Add images" to pick from the media library — uploads happen there too.', 200, 'admin', '商品图集多图控件', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pMediaAdd', 'admin.products.media.add'), ('pMediaAltPh', 'admin.products.media.altPlaceholder'), ('pMediaEmpty', 'admin.products.media.empty')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
