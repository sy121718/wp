-- 572 · 悬浮球的图片附件词条（粘贴 / 选图的按钮与三条校验失败提示）。
--
-- 为什么单开一条而不并进 570：570 已经在开发库与其他库上执行过，它的判据
-- （枚举 thinking / thinkLabel / stop / failed）在那些库上已经满足 → 整条迁移
-- 会被判为已完成而跳过，新加的 key 永远进不去。迁移一旦发布过就不能追加内容。
--
-- 两侧命名空间各自独立（570 同一课）：
--   · 模板侧 admin.ai.fab.*（跟着语言走，由模板直接取）
--   · handler 侧 ai.fab.*（由 internal/module/ai/enums 的 MsgFab* 常量给出）
-- 只 seed 一边会让另一边在页面上显示成 key 字面量。
--
-- 幂等：ON CONFLICT DO NOTHING；判据取本批自己的五个 key（模板侧两个 + handler 侧三个）。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.ai.fab.pickImage', 'zh-CN', '加图片'),
('admin.ai.fab.pickImage', 'en-US', 'Add image'),
('admin.ai.fab.imageRemove', 'zh-CN', '移除这张图'),
('admin.ai.fab.imageRemove', 'en-US', 'Remove this image'),
('ai.fab.imageBadFormat', 'zh-CN', '图片格式不认识，只支持直接粘贴或选择的图片文件。'),
('ai.fab.imageBadFormat', 'en-US', 'Unrecognized image format. Paste or pick an image file directly.'),
('ai.fab.imageTooMany', 'zh-CN', '一次最多带 4 张图。'),
('ai.fab.imageTooMany', 'en-US', 'At most 4 images per question.'),
('ai.fab.imageTooLarge', 'zh-CN', '有图片太大了（单张上限约 3MB），请换一张小的。'),
('ai.fab.imageTooLarge', 'en-US', 'An image is too large (about 3MB each). Please use a smaller one.')
ON CONFLICT (item_key, lang) DO NOTHING;
