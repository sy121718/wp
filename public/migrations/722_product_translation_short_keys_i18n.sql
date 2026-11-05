-- 商品翻译界面完整短词条；运营译文与额外语言原子迁移，碎片来源只保留数据不再拼句。
WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.product_translations.col.field', 'trField', true),
    ('admin.product_translations.col.origin', 'trOrigin', true),
    ('admin.product_translations.col.source', 'trSrc', true),
    ('admin.product_translations.col.state', 'trState', true),
    ('admin.product_translations.col.target', 'trTargetCol', true),
    ('admin.product_translations.empty', 'trEmpty', true),
    ('admin.product_translations.emptyTitle', 'trEmptyTitle', true),
    ('admin.product_translations.entityType.attribute', 'trTypeAttr', true),
    ('admin.product_translations.entityType.brand', 'trTypeBrand', true),
    ('admin.product_translations.entityType.category', 'trTypeCat', true),
    ('admin.product_translations.entityType.tag', 'trTypeTag', true),
    ('admin.product_translations.field.attributeValues', 'trFieldAttrVals', true),
    ('admin.product_translations.field.productDescription', 'trFieldProdDesc', true),
    ('admin.product_translations.field.productImageAlts', 'trProductAlts', true),
    ('admin.product_translations.field.productName', 'trFieldProdName', true),
    ('admin.product_translations.field.productSubtitle', 'trFieldProdSub', true),
    ('admin.product_translations.help.label', 'trHelp', true),
    ('admin.product_translations.label.lang', 'trLang', true),
    ('admin.product_translations.label.project', 'trProj', true),
    ('admin.product_translations.noTargetLang.action', 'trNoTgtLangAct', true),
    ('admin.product_translations.noTargetLang.title', 'trNoLangTitle', true),
    ('admin.product_translations.noscriptSwitch', 'trNoJsSwitch', true),
    ('admin.product_translations.ph.target', 'trTargetPh', true),
    ('admin.product_translations.richHint', 'trRichHint', true),
    ('admin.product_translations.saveAll', 'trSaveAll', true),
    ('admin.product_translations.saveHint', 'trSaveHint', true),
    ('admin.product_translations.state.missing', 'trStateMiss', true),
    ('admin.product_translations.state.translated', 'trStateDone', true),
    ('admin.product_translations.title', 'trTitle', true),
    ('admin.product_translations.viewProject', 'trViewProj', true)
), guard AS MATERIALIZED (
    SELECT CASE WHEN COUNT(*) = 0 THEN 1
        ELSE ('商品短词条冲突: ' || string_agg(k.old_key || ' -> ' || k.new_key || '/' || s.lang, ', '))::integer END AS ok
    FROM keys k
    JOIN sys_i18n s ON s.item_key = k.old_key
    JOIN sys_i18n t ON t.item_key = k.new_key AND t.lang = s.lang
    WHERE ROW(t.item_value, t.http_code, t.category, t.remark, t.status)
        IS DISTINCT FROM ROW(s.item_value, s.http_code, s.category, s.remark, s.status)
), moved AS (
    UPDATE sys_i18n s SET item_key = k.new_key
    FROM keys k, guard g
    WHERE g.ok = 1 AND k.retire AND s.item_key = k.old_key
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
), copied AS (
    INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
    SELECT k.new_key, s.lang, s.item_value, s.http_code, s.category, s.remark, s.status, s.create_time, s.update_time
    FROM keys k JOIN sys_i18n s ON s.item_key = k.old_key CROSS JOIN guard g
    WHERE g.ok = 1 AND NOT k.retire
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING id
), retired AS (
    DELETE FROM sys_i18n s USING keys k, guard g
    WHERE g.ok = 1 AND k.retire AND s.item_key = k.old_key
      AND EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
)
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
    ('trField', 'zh-CN', '字段', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trField', 'en-US', 'Field', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trOrigin', 'zh-CN', '来源', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trOrigin', 'en-US', 'Origin', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trSrc', 'zh-CN', '原文', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trSrc', 'en-US', 'Source', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trState', 'zh-CN', '状态', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trState', 'en-US', 'Status', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trTargetCol', 'zh-CN', '译文', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trTargetCol', 'en-US', 'Translation', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trEmpty', 'zh-CN', '没有可翻译文本（只有白名单字段参与翻译；纯数字 / 纯符号与链接、色值等字段不翻译）。', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trEmpty', 'en-US', 'Nothing to translate here (only whitelisted fields are translated; pure numbers, pure symbols, links and colour values are left alone).', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trEmptyTitle', 'zh-CN', '没有可翻译文本', 200, 'admin', '第四轮后台页面改造新增文案位', 1, now(), now()),
    ('trEmptyTitle', 'en-US', 'No text to translate', 200, 'admin', '第四轮后台页面改造新增文案位', 1, now(), now()),
    ('trTypeAttr', 'zh-CN', '商品属性', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeAttr', 'en-US', 'Product attribute', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeBrand', 'zh-CN', '商品品牌', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeBrand', 'en-US', 'Product brand', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeCat', 'zh-CN', '商品分类', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeCat', 'en-US', 'Product category', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeTag', 'zh-CN', '商品标签', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trTypeTag', 'en-US', 'Product tag', 200, 'admin', 'translatable entity type label', 1, now(), now()),
    ('trFieldAttrVals', 'zh-CN', '属性值展示文本', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldAttrVals', 'en-US', 'Attribute value display text', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldProdDesc', 'zh-CN', '描述', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldProdDesc', 'en-US', 'Description', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trProductAlts', 'zh-CN', '图片 alt', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trProductAlts', 'en-US', 'Image alt', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldProdName', 'zh-CN', '商品名', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldProdName', 'en-US', 'Product name', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldProdSub', 'zh-CN', '副标题', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trFieldProdSub', 'en-US', 'Subtitle', 200, 'admin', 'translatable field label', 1, now(), now()),
    ('trHelp', 'zh-CN', '查看说明', 200, 'admin', '第四轮后台页面改造新增文案位', 1, now(), now()),
    ('trHelp', 'en-US', 'Show help', 200, 'admin', '第四轮后台页面改造新增文案位', 1, now(), now()),
    ('trLang', 'zh-CN', '语言', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trLang', 'en-US', 'Language', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trProj', 'zh-CN', '工程', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trProj', 'en-US', 'Project', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trNoTgtLangAct', 'zh-CN', '去站点设置启用更多语言', 200, 'product', 'internal/templates/admin', 1, now(), now()),
    ('trNoTgtLangAct', 'en-US', 'Enable more languages in site settings', 200, 'product', 'internal/templates/admin', 1, now(), now()),
    ('trNoLangTitle', 'zh-CN', '没有可翻译的目标语言', 200, 'product', 'internal/templates/admin', 1, now(), now()),
    ('trNoLangTitle', 'en-US', 'No target language to translate into', 200, 'product', 'internal/templates/admin', 1, now(), now()),
    ('trNoJsSwitch', 'zh-CN', '切换', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trNoJsSwitch', 'en-US', 'Switch', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trTargetPh', 'zh-CN', '填写译文（留空则不写入）', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trTargetPh', 'en-US', 'Enter the translation (leave blank to skip this row)', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trRichHint', 'zh-CN', '原文含 HTML 标签时译文也必须含标签（形态一致）', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trRichHint', 'en-US', 'When the source contains HTML tags, the translation must contain them too (keep the shape identical)', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trSaveAll', 'zh-CN', '保存全部', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trSaveAll', 'en-US', 'Save all', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trSaveHint', 'zh-CN', '写入 sys_translation（来源 manual）并按同一语境的 hash 寻址；改原文后旧译文自动失效。清空输入框表示「本行不写入」，不会删除已有译文。', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trSaveHint', 'en-US', 'Writes to sys_translation (origin manual), addressed by the hash of the same context; editing the source invalidates the old translation automatically. Clearing a box means "do not write this row" — it never deletes an existing translation.', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trStateMiss', 'zh-CN', '缺失', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trStateMiss', 'en-US', 'Missing', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trStateDone', 'zh-CN', '已翻译', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trStateDone', 'en-US', 'Translated', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trTitle', 'zh-CN', '商品多语言', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trTitle', 'en-US', 'Product translations', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trViewProj', 'zh-CN', '查看整个工程', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trViewProj', 'en-US', 'View the whole project', 200, 'admin', 'admin/product_translations.html', 1, now(), now()),
    ('trScopeInfo', 'zh-CN', '商品域可翻译文本：商品名 / 副标题 / 描述 / 图片 alt、分类名与描述、品牌名与描述、标签名、属性组名与属性值展示文本。slug / SKU / 条码 / 价格 / 属性值 key 不参与翻译（它们进 URL、筛选参数与规格组合）。无译文时产物逐字节回退原文。', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trScopeInfo', 'en-US', 'Translatable product text: product names, subtitles, descriptions, image alt text, category and brand names and descriptions, tag names, attribute names and display values. Slugs, SKUs, barcodes, prices and attribute value keys are not translated because they are used in URLs, filters and variant combinations. Published output falls back to the original text byte for byte when no translation exists.', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trProgress', 'zh-CN', '完成度 {done} / {total} 条', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trProgress', 'en-US', 'Translated: {done} / {total} entries', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trGroupInfo', 'zh-CN', '（{count} 条 · {type}）', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trGroupInfo', 'en-US', '({count} entries · {type})', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trNoTargetDesc', 'zh-CN', '站点语言清单里只有默认语言（翻译的源语言），没有第二种语言可以作为翻译目标。', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trNoTargetDesc', 'en-US', 'The site language list contains only the default language, which is the source language. There is no second language to translate into.', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trSourceOnly', 'zh-CN', '站点语言清单里只有默认语言「{lang}」（翻译的源语言），没有第二种语言可以作为翻译目标。', 0, 'product', '商品翻译完整句', 1, now(), now()),
    ('trSourceOnly', 'en-US', 'The site language list contains only the default language "{lang}", which is the source language. There is no second language to translate into.', 0, 'product', '商品翻译完整句', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
LEFT JOIN keys k ON k.new_key = d.item_key CROSS JOIN guard g
WHERE g.ok = 1
  AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = d.item_key AND t.lang = d.lang)
  AND NOT EXISTS (SELECT 1 FROM sys_i18n s WHERE s.item_key = k.old_key AND s.lang = d.lang);
