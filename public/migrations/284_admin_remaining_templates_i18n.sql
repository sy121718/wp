-- 284 · 后台模板剩余硬编码文案的词条（product_detail / products_new / mail_marketing）
--
-- 背景：`scripts/check-i18n-coverage.sh` 在 2026-09-19 第五批提交后变红（基线 2 / 实测 19）——
-- 19 行未 key 化的中文文案随那次提交进了仓库。本迁移补齐它对应的全部词条（模板侧已同批改成
-- `{{ .["t"]("key", "中文兜底") }}`），并把基线**下调为 0**（门槛只降不升）。
--
-- 为什么基线原本是 2、现在能到 0：那 2 行是更早就存在的未 key 化文案（历史基线），
-- 本批一并清掉 —— 基线是可收紧的债务清单，不是许愿池。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，后台是真相来源）。
-- 判定枚举本批**全部 21 个 key**（>=21）：用全库行数会被同期其它批次满足而静默跳过。
--
-- 注册：public/migrations/register.go 的 init() 调 registerAdminRemainingTemplatesI18n()。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('admin.product_detail.template.mode.document', 'zh-CN', '独立文档', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.mode.document', 'en-US', 'Standalone document', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.mode.template', 'zh-CN', '跟随模板', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.mode.template', 'en-US', 'Follows template', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.current', 'zh-CN', '当前模板', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.current', 'en-US', 'Current template', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.customize', 'zh-CN', '进入自定义（仅此商品）', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.customize', 'en-US', 'Customize (this product only)', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.edit', 'zh-CN', '编辑模板', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.edit', 'en-US', 'Edit template', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.affectedLead', 'zh-CN', '（影响 ', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.affectedLead', 'en-US', ' (affects ', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.affectedTail', 'zh-CN', ' 个商品）', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.affectedTail', 'en-US', ' products)', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.docHint', 'zh-CN', '这是该商品自己的文档：改动只影响这一个商品，模板更新不会同步过来。', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.docHint', 'en-US', 'This document belongs to this product: changes affect only this product, and template updates are not synced here.', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.presetUpdated', 'zh-CN', '模板已有新版本 —— 点「重新套用预设」即可跟进（会放弃本商品的独立改动）。', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.presetUpdated', 'en-US', 'A newer template version exists — click "Reapply preset" to follow it (discards this product standalone changes).', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.reapplyPreset', 'zh-CN', '重新套用预设（回到跟随模板）', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.reapplyPreset', 'en-US', 'Reapply preset (back to following the template)', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.rollbackLabel', 'zh-CN', '回滚到历史版本', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.rollbackLabel', 'en-US', 'Roll back to a historical version', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.rollbackSubmit', 'zh-CN', '回滚文档并重发', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.rollbackSubmit', 'en-US', 'Roll back document and republish', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.rollbackHint', 'zh-CN', '回滚取该历史版本的文档重新发布（实体数据取最新）。', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.rollbackHint', 'en-US', 'Rollback republishes the document from that historical version (entity data is taken from the latest).', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.presetModeHint', 'zh-CN', '布局改动请到模板编辑器：改一次，跟随该模板的商品一起更新。想把某个商品单独改，保存结构改动时会提示你转为独立文档。', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.presetModeHint', 'en-US', 'Make layout changes in the template editor: edit once and every product following that template updates together. To change one product alone, saving structural changes will offer to convert it into a standalone document.', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.unpublishedHint', 'zh-CN', '模板已绑定但尚未发布：先在上方完成首次发布，再进入可视化自定义。', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.unpublishedHint', 'en-US', 'A template is bound but not published yet: finish the first publish above, then open the visual customizer.', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.noPublicationHint', 'zh-CN', '该商品还没有发布详情页：完成首次发布后可在此预览与进入可视化自定义。', 'admin', '', 1, 200, now(), now()),
    ('admin.product_detail.template.noPublicationHint', 'en-US', 'This product has no published detail page yet: after the first publish you can preview and open the visual customizer here.', 'admin', '', 1, 200, now(), now()),
    ('admin.products.new.template.boundHint', 'zh-CN', '创建后在商品详情页绑定模板、预览并进入可视化自定义（只影响该商品）。', 'admin', '', 1, 200, now(), now()),
    ('admin.products.new.template.boundHint', 'en-US', 'After creating, bind a template on the product detail page, preview, and open the visual customizer (this product only).', 'admin', '', 1, 200, now(), now()),
    ('admin.products.new.template.noneHint', 'zh-CN', '暂无商品详情模板，将使用默认结构发布。', 'admin', '', 1, 200, now(), now()),
    ('admin.products.new.template.noneHint', 'en-US', 'No product detail template yet; the default structure will be used for publishing.', 'admin', '', 1, 200, now(), now()),
    ('admin.products.new.template.unavailableHint', 'zh-CN', '模板能力未装配，商品将使用默认结构发布。', 'admin', '', 1, 200, now(), now()),
    ('admin.products.new.template.unavailableHint', 'en-US', 'Template capability is not wired; the product will be published with the default structure.', 'admin', '', 1, 200, now(), now()),
    ('admin.mail.marketing.import.tags.ph', 'zh-CN', 'vip,华南', 'admin', '', 1, 200, now(), now()),
    ('admin.mail.marketing.import.tags.ph', 'en-US', 'vip,APAC', 'admin', '', 1, 200, now(), now()),
    ('admin.mail.marketing.import.content.ph', 'zh-CN', 'a@example.com&#10;b@example.com&#10;或 CSV：email,name,tags', 'admin', '', 1, 200, now(), now()),
    ('admin.mail.marketing.import.content.ph', 'en-US', 'a@example.com&#10;b@example.com&#10;or CSV: email,name,tags', 'admin', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
