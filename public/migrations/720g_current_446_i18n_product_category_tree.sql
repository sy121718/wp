-- 当前词条回补源；历史原文 446_i18n_product_category_tree.sql 保持归档，不再执行。
-- Product category tree expansion and bounded parent-picker feedback.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('admin.product_categories.search.matchCount', 'zh-CN', '匹配', 200, 'admin', 'product category tree search result count prefix', 1, now(), now()),
('admin.product_categories.search.matchCount', 'en-US', 'Matched', 200, 'admin', 'product category tree search result count prefix', 1, now(), now()),
('admin.product_categories.search.matchSuffix', 'zh-CN', '条；上级路径仅供定位', 200, 'admin', 'product category tree search result count suffix', 1, now(), now()),
('admin.product_categories.search.matchSuffix', 'en-US', 'results; ancestor paths are shown for context only', 200, 'admin', 'product category tree search result count suffix', 1, now(), now()),
('catFindMatched', 'zh-CN', '匹配', 200, 'admin', 'product category tree matched-row badge', 1, now(), now()),
('catFindMatched', 'en-US', 'Match', 200, 'admin', 'product category tree matched-row badge', 1, now(), now()),
('catFindAncestor', 'zh-CN', '上级路径', 200, 'admin', 'product category tree ancestor context label', 1, now(), now()),
('catFindAncestor', 'en-US', 'Ancestor path', 200, 'admin', 'product category tree ancestor context label', 1, now(), now()),
('admin.product_categories.children.empty', 'zh-CN', '没有子分类', 200, 'admin', 'product category child fragment empty state', 1, now(), now()),
('admin.product_categories.children.empty', 'en-US', 'No child categories', 200, 'admin', 'product category child fragment empty state', 1, now(), now()),
('admin.product_categories.children.paginationLabel', 'zh-CN', '子分类分页', 200, 'admin', 'product category child fragment pagination label', 1, now(), now()),
('admin.product_categories.children.paginationLabel', 'en-US', 'Child category pagination', 200, 'admin', 'product category child fragment pagination label', 1, now(), now()),
('admin.product_categories.children.pagePrefix', 'zh-CN', '第', 200, 'admin', 'product category child fragment page prefix', 1, now(), now()),
('admin.product_categories.children.pagePrefix', 'en-US', 'Page', 200, 'admin', 'product category child fragment page prefix', 1, now(), now()),
('admin.product_categories.children.pageSuffix', 'zh-CN', '页', 200, 'admin', 'product category child fragment page suffix', 1, now(), now()),
('admin.product_categories.children.pageSuffix', 'en-US', '', 200, 'admin', 'product category child fragment page suffix', 1, now(), now()),
('catParentFind', 'zh-CN', '搜索父级分类', 200, 'admin', 'product category parent picker search label', 1, now(), now()),
('catParentFind', 'en-US', 'Search parent category', 200, 'admin', 'product category parent picker search label', 1, now(), now()),
('catParentFindPh', 'zh-CN', '按名称搜索父级', 200, 'admin', 'product category parent picker search placeholder', 1, now(), now()),
('catParentFindPh', 'en-US', 'Search parents by name', 200, 'admin', 'product category parent picker search placeholder', 1, now(), now()),
('catParentEmpty', 'zh-CN', '没有匹配的分类', 200, 'admin', 'product category parent picker empty result', 1, now(), now()),
('catParentEmpty', 'en-US', 'No matching categories', 200, 'admin', 'product category parent picker empty result', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('catParentEmpty', 'admin.product_categories.parentSearch.empty'), ('catParentFind', 'admin.product_categories.parentSearch.label'), ('catParentFindPh', 'admin.product_categories.parentSearch.placeholder'), ('catFindAncestor', 'admin.product_categories.search.ancestor'), ('catFindMatched', 'admin.product_categories.search.matched')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
