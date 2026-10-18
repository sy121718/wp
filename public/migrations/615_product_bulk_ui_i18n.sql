-- 615 · 商品模块 JSON 写表单的界面词条（批量回执、行内保存、筛选刷新）。
--
-- 为什么现在补：这些 key 在模板里已经用了（data-msg-* 属性给前端 toast 用），但从未 seed ——
-- `check-i18n-keys-seeded.sh` 的判据是「模板引用了、库里没有」，34 个 key 一直红着。
-- 未 seed 的表现不是报错：取词命中不到就回落到模板里的中文兜底，英文界面于是整段是中文，
-- 而页面 200、无日志。所以「模板写了兜底」不能当作「不用 seed」。
--
-- 命名归属：admin.products.* / admin.product_attributes.* / admin.product_brands.* /
-- admin.product_categories.* / admin.product_tags.* 各自跟自己的页面，跨页通用的两条留在
-- admin.common.*（刷新列表）。本批**不新增权限点、不改菜单**。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register_product_bulk_i18n.go 里
-- 逐条枚举本批 34 个 item_key（上界封闭，34 × 2 = 68 行）—— 不用 LIKE 前缀、不用全库总量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.common.write.refresh', 'zh-CN', '刷新列表', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.common.write.refresh', 'en-US', 'Refresh list', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulk.noneSelected', 'zh-CN', '没有勾选任何属性组，列表未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulk.noneSelected', 'en-US', 'No attribute groups selected; the list is unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.allDeleted', 'zh-CN', '已删除 {count} 个属性组。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.allDeleted', 'en-US', 'Deleted {count} attribute groups.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.allSkipped', 'zh-CN', '{count} 个属性组都未能删除。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.allSkipped', 'en-US', '{count} attribute groups could not be deleted.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.partial', 'zh-CN', '已删除 {deleted} 个属性组，{skipped} 个未能删除。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.partial', 'en-US', 'Deleted {deleted} attribute groups; {skipped} could not be deleted.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.refresh', 'zh-CN', '刷新列表', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.bulkDelete.refresh', 'en-US', 'Refresh list', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.list.deleted', 'zh-CN', '属性组已删除。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.list.deleted', 'en-US', 'Attribute group deleted.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.saved', 'zh-CN', '已保存。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.saved', 'en-US', 'Saved.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.values.saved', 'zh-CN', '属性值已保存。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_attributes.values.saved', 'en-US', 'Attribute values saved.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulk.noneSelected', 'zh-CN', '没有勾选任何品牌，列表未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulk.noneSelected', 'en-US', 'No brands selected; the list is unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.allDeleted', 'zh-CN', '已删除 {count} 个品牌。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.allDeleted', 'en-US', 'Deleted {count} brands.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.allSkipped', 'zh-CN', '{count} 个品牌都未能删除（仍被商品引用）。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.allSkipped', 'en-US', '{count} brands could not be deleted (still referenced by products).', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.partial', 'zh-CN', '已删除 {deleted} 个品牌，{skipped} 个未能删除（仍被商品引用）。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.partial', 'en-US', 'Deleted {deleted} brands; {skipped} could not be deleted (still referenced by products).', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.refresh', 'zh-CN', '刷新列表', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_brands.bulkDelete.refresh', 'en-US', 'Refresh list', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulk.noneSelected', 'zh-CN', '没有勾选任何分类，列表未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulk.noneSelected', 'en-US', 'No categories selected; the list is unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.allDeleted', 'zh-CN', '已删除 {count} 个分类。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.allDeleted', 'en-US', 'Deleted {count} categories.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.allSkipped', 'zh-CN', '{count} 个分类都未能删除（有子分类或仍被商品引用）。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.allSkipped', 'en-US', '{count} categories could not be deleted (they have children or are still referenced by products).', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.partial', 'zh-CN', '已删除 {deleted} 个分类，{skipped} 个未能删除（有子分类或仍被商品引用）。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.partial', 'en-US', 'Deleted {deleted} categories; {skipped} could not be deleted (they have children or are still referenced by products).', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.refresh', 'zh-CN', '刷新列表', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_categories.bulkDelete.refresh', 'en-US', 'Refresh list', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulk.noneSelected', 'zh-CN', '没有勾选任何标签，列表未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulk.noneSelected', 'en-US', 'No tags selected; the list is unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.allDeleted', 'zh-CN', '已删除 {count} 个标签。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.allDeleted', 'en-US', 'Deleted {count} tags.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.allSkipped', 'zh-CN', '{count} 个标签都未能删除。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.allSkipped', 'en-US', '{count} tags could not be deleted.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.partial', 'zh-CN', '已删除 {deleted} 个标签，{skipped} 个未能删除。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.partial', 'en-US', 'Deleted {deleted} tags; {skipped} could not be deleted.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.refresh', 'zh-CN', '刷新列表', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.bulkDelete.refresh', 'en-US', 'Refresh list', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.list.deleted', 'zh-CN', '标签已删除。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.list.deleted', 'en-US', 'Tag deleted.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.saved', 'zh-CN', '已保存。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.product_tags.saved', 'en-US', 'Saved.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulk.noneSelected', 'zh-CN', '没有勾选任何商品，列表未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulk.noneSelected', 'en-US', 'No products selected; the list is unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.allSkipped', 'zh-CN', '{count} 个商品都未能改价，价格未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.allSkipped', 'en-US', '{count} products could not be repriced; prices are unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.allUpdated', 'zh-CN', '已按规则改价 {count} 个商品。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.allUpdated', 'en-US', 'Repriced {count} products.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.noneSelected', 'zh-CN', '没有勾选任何商品，调价未执行。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.noneSelected', 'en-US', 'No products selected; bulk pricing was not run.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.partial', 'zh-CN', '已改价 {updated} 个，{skipped} 个未能改价（可能已被删除或不在本工程）。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkPricing.partial', 'en-US', 'Repriced {updated}; {skipped} could not be repriced (maybe deleted or outside this project).', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkResult.allDeleted', 'zh-CN', '已删除 {count} 个商品及其全部变体。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkResult.allDeleted', 'en-US', 'Deleted {count} products and all their variants.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkResult.allSkipped', 'zh-CN', '{count} 个商品都未能删除，列表未改动。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkResult.allSkipped', 'en-US', '{count} products could not be deleted; the list is unchanged.', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkResult.partial', 'zh-CN', '已删除 {deleted} 个，{skipped} 个未能删除（可能已被删除或不在本工程）。', 200, 'ui', 'admin product bulk writes', 1, now(), now()),
('admin.products.bulkResult.partial', 'en-US', 'Deleted {deleted}; {skipped} could not be deleted (maybe deleted or outside this project).', 200, 'ui', 'admin product bulk writes', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
