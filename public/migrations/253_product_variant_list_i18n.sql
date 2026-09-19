-- 253 · 变体清单「预览—保存」模型的词条（docs/14 §8，2026-09-19 用户拍板）。
--
-- 本批新增的 enums 常量值就是 i18n key，真文案在这张表里；缺词条的后果是页面上原样显示
-- ErrVariantSKUEmpty 这种裸 key（既不中文也不是话），所以新增常量必须同批 seed。
--
-- 六个 key 分两类：
--   · 两条业务错误（ErrVariantSKUEmpty / ErrVariantOptionsInvalid）——「保存」时服务端重算
--     清单行得出的结论，必须能读；
--   · 四个跳过原因（VariantSkip*）—— 清单外要删的既有变体因仍有库存 / 被 BOM 引用 /
--     行重复 / 变体已被别处删除而被跳过，逐条回带（不整批失败、不静默）。
--
-- 同时 seed 本批模板新增的文案位（商品详情页的清单区块与生成组合抽屉）：中文站点有模板
-- 兜底看起来正常，英文站点会退回中文兜底 —— 真源是本表，所以一并补上。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrVariantSKUEmpty', 'zh-CN', '变体的 SKU 编码不能为空：请填写一个编码（或改回系统生成的编码）', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrVariantSKUEmpty', 'en-US', 'Variant SKU code cannot be empty: fill in a code (or restore the system-generated one)', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrVariantOptionsInvalid', 'zh-CN', '变体的规格组合不合法：属性组或属性值不属于该商品', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrVariantOptionsInvalid', 'en-US', 'Invalid variant option combination: the attribute group or value does not belong to this product', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipHasStock', 'zh-CN', '仍有库存，未删除（请先处理库存或改为停用）', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipHasStock', 'en-US', 'Still has stock, not deleted (handle the stock first, or disable it instead)', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipReferenced', 'zh-CN', '被 BOM 清单引用，未删除（请先解除引用）', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipReferenced', 'en-US', 'Referenced by a BOM, not deleted (remove the reference first)', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipDuplicated', 'zh-CN', '与清单里前面的行重复，已忽略', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipDuplicated', 'en-US', 'Duplicate of an earlier row in the list, ignored', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipVariantMissing', 'zh-CN', '这一行对应的变体已不存在（可能已被别处删除）', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('VariantSkipVariantMissing', 'en-US', 'The variant for this row no longer exists (it may have been deleted elsewhere)', 200, 'product', 'internal/module/product/enums', 1, now(), now()),
('admin.products.lastNotice', 'zh-CN', '上一次保存结果：', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.lastNotice', 'en-US', 'Last save result: ', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variants.listHint', 'zh-CN', '清单里的「生成 / 移除」都不写库：生成只是把组合显示在这里，「保存变体清单」才落库。', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variants.listHint', 'en-US', 'Generating and removing rows write nothing to the database; only "Save variant list" persists the list.', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.skuLabel', 'zh-CN', 'SKU 编码（可编辑）', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.skuLabel', 'en-US', 'SKU code (editable)', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.remove', 'zh-CN', '移除', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.remove', 'en-US', 'Remove', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.saveList', 'zh-CN', '保存变体清单', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.saveList', 'en-US', 'Save variant list', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.saveWarehouse', 'zh-CN', '新增变体的归属仓', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.saveWarehouse', 'en-US', 'Home warehouse for new variants', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.pendingSave', 'zh-CN', '保存后生效', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.pendingSave', 'en-US', 'Applied after saving', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.saveFailed', 'zh-CN', '清单序列化失败，请刷新页面后重试。', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.variant.saveFailed', 'en-US', 'Failed to serialize the list; refresh the page and try again.', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.previewHint', 'zh-CN', '生成只是把组合显示在清单里，点「保存变体清单」才写入数据库。', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.previewHint', 'en-US', 'Generating only shows the combinations in the list; the database is written only when you save the variant list.', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.added', 'zh-CN', '已追加 %d 行到清单（还没保存）', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.added', 'en-US', '%d row(s) appended to the list (not saved yet)', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.none', 'zh-CN', '没有需要追加的组合：清单与库里都已有', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.none', 'en-US', 'No combinations to append: the list and the database already have them all', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.failed', 'zh-CN', '生成失败：', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.products.combo.failed', 'en-US', 'Generation failed: ', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
