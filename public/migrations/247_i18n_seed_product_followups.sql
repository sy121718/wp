-- 247 · 商品域后续词条：捆绑构成区块 / 多语言空态 / 新建抽屉属性组多选 / 批量改价（29 key × 2 语言）
--
-- 这三批一起 seed：S4 详情页捆绑构成（19）、S5 商品多语言空态（4）、S1 新建抽屉与批量改价（6）。
-- 它们此前只有模板里的中文兜底 —— 中文站点看起来正常，英文站点会显示中文（兜底文案），
-- 而 enums/模板 key 的真源是本表，所以补在这里。
--
-- 同时退役一个旧词条：admin.products.ph.attributeIds（属性引用文本框时代的占位符，
-- 改版后不再被任何模板取用）。按约定不回改历史迁移（191），只在这里把 status 置 0
-- 留痕，避免它继续出现在文案词条页的可选清单里。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；退役用 UPDATE ... WHERE status <> 0。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_translations.noTargetLang.action', 'zh-CN', '去站点设置启用更多语言', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.action', 'en-US', 'Enable more languages in site settings', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.desc', 'zh-CN', '站点语言清单里只有默认语言', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.desc', 'en-US', 'The site language list contains only the default language', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.descTail', 'zh-CN', '（它就是翻译的源语言），没有第二种语言可以作为翻译目标。', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.descTail', 'en-US', ' (the translation source), so there is no second language to translate into.', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.title', 'zh-CN', '没有可翻译的目标语言', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.product_translations.noTargetLang.title', 'en-US', 'No target language to translate into', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.bulkPricing.previewHintLead', 'zh-CN', '想先看每个 SKU 的「原价 → 新价」，去', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.bulkPricing.previewHintLead', 'en-US', 'To preview the old to new price of each SKU first, use the', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.bulkPricing.previewHintTail', 'zh-CN', '页试算（不落库）。', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.bulkPricing.previewHintTail', 'en-US', 'page as a dry run (nothing is written).', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.bulkPricing.scopeHint', 'zh-CN', '作用范围是列表里勾选的商品：每个商品的全部变体都按同一条规则改价。只想改一部分时，先回列表取消勾选那几个。', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.bulkPricing.scopeHint', 'en-US', 'The scope is the products ticked in the list: every variant of each product is repriced by the same rule. To reprice only some of them, go back to the list and untick the others.', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.create.attributes', 'zh-CN', '引用的属性组（可多选，可留空）', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.create.attributes', 'en-US', 'Referenced attribute groups (multi-select, optional)', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.create.noAttributesLead', 'zh-CN', '这个工程还没有属性组，先到', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.create.noAttributesLead', 'en-US', 'This project has no attribute groups yet. Create one on the', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.create.noAttributesTail', 'zh-CN', '页建一个（可留空，之后在商品详情页也能挂）。', 200, 'product', 'internal/templates/admin', 1, now(), now()),
('admin.products.create.noAttributesTail', 'en-US', 'page first (optional; you can also attach them later on the product detail page).', 200, 'product', 'internal/templates/admin', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

UPDATE sys_i18n SET status = 0, update_time = now()
 WHERE item_key = 'admin.products.ph.attributeIds' AND status <> 0;
