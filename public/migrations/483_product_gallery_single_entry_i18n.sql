-- 483 · 商品图集的添加入口收成一处，空态文案随之改口。
--
-- 背景：图集控件原来有两个同动作的添加入口 —— 标题行右上角的「添加图片」按钮与网格末尾的
--   虚线「＋」框。两个入口分处标题行与网格，屏幕上像两件事（占位也大），同日删掉按钮只留「＋」；
--   空态那句原本指着「添加图片」按钮说话，现在改指「＋」框。
--   add 那条词条仍在使用（「＋」框的 aria-label），本批不动。
--
-- 幂等：只改 478 插入的**原始值** —— 运维改过的文案算既定事实，不回改。
UPDATE sys_i18n
SET item_value = '还没有图片 —— 点网格里的「＋」从媒体库里挑，上传也在媒体库里做。',
    update_time = now()
WHERE item_key = 'admin.products.media.empty'
  AND lang = 'zh-CN'
  AND item_value = '还没有图片。点「添加图片」从媒体库里挑 —— 上传也在媒体库里做。';

UPDATE sys_i18n
SET item_value = 'No images yet — use the "+" tile to pick from the media library; uploads happen there too.',
    update_time = now()
WHERE item_key = 'admin.products.media.empty'
  AND lang = 'en-US'
  AND item_value = 'No images yet. Use "Add images" to pick from the media library — uploads happen there too.';
