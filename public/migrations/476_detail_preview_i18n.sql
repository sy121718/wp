-- 476 · 详情页真实预览（文章 / 商品编辑页）的界面文案。
--
-- 背景：编辑页的预览原来是「把正文拼进 iframe 的白底排版」，看不出发布后长什么样。
--   现在改成用**详情页模板**渲染（presentation.PreviewInstance，不写库不激活），
--   于是文案要说清两件新事：① 这是访客看到的样子；② 它只反映**已保存**的数据。
--   第二条尤其重要 —— 不说的话，改完正文看预览没变化会被当成「预览坏了」。
--
-- 归属：文章编辑页（admin/content/article_edit.html）、商品编辑页（admin/product/product_edit.html），
--   以及两个预览帧 handler 的替代页（无实体 / 无模板 / 渲染失败时的三句话）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 14 个 key（×2 语言 = 28 行）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.article.edit.previewRealHeading', 'zh-CN', '详情页预览', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.edit.previewRealHeading', 'en-US', 'Detail preview', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.edit.previewRealHint', 'zh-CN', '这里用详情页模板渲染这篇文章，看到的就是访客看到的样子。它只反映**已保存**的内容：改完正文先点保存，再点下面的「刷新预览」。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.edit.previewRealHint', 'en-US', 'This renders the article with its detail template, so you see what visitors see. It reflects **saved** content only: save first, then hit "Refresh preview".', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.edit.previewRefresh', 'zh-CN', '刷新预览', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.edit.previewRefresh', 'en-US', 'Refresh preview', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.needSave', 'zh-CN', '保存这篇文章后，这里会显示它的详情页真实形态。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.needSave', 'en-US', 'Save this article to see its real detail-page form here.', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.unavailable', 'zh-CN', '预览不可用：发布能力未装配。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.unavailable', 'en-US', 'Preview unavailable: the publishing capability is not wired.', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.failed', 'zh-CN', '预览渲染失败（多半是这篇还没有可用的详情页模板）。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.failed', 'en-US', 'Preview failed to render (most likely this article has no usable detail template yet).', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.empty', 'zh-CN', '还没有可渲染的详情页：先给 article 类型建一套内容模板。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.article.preview.empty', 'en-US', 'Nothing to render yet: create a content template of type article first.', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.heading', 'zh-CN', '详情页预览', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.heading', 'en-US', 'Detail preview', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.hint', 'zh-CN', '这里用详情页模板渲染这个商品，看到的就是访客看到的样子（图集、变体分组、关联分区都在）。它只反映**已保存**的内容：改完先点保存，再点「刷新预览」。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.hint', 'en-US', 'This renders the product with its detail template, so you see what visitors see (gallery, variant groups, related sections). It reflects **saved** content only: save first, then hit "Refresh preview".', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.refresh', 'zh-CN', '刷新预览', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.refresh', 'en-US', 'Refresh preview', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.needSave', 'zh-CN', '保存这个商品后，这里会显示它的详情页真实形态。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.needSave', 'en-US', 'Save this product to see its real detail-page form here.', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.unavailable', 'zh-CN', '预览不可用：发布能力未装配。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.unavailable', 'en-US', 'Preview unavailable: the publishing capability is not wired.', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.failed', 'zh-CN', '预览渲染失败（多半是这个商品还没有可用的详情页模板）。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.failed', 'en-US', 'Preview failed to render (most likely this product has no usable detail template yet).', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.empty', 'zh-CN', '还没有可渲染的详情页：先给 product 类型建一套内容模板。', 200, 'admin', '详情页真实预览', 1, now(), now()),
('admin.products.preview.empty', 'en-US', 'Nothing to render yet: create a content template of type product first.', 200, 'admin', '详情页真实预览', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
