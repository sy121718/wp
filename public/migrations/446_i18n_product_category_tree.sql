-- Product category tree expansion and bounded parent-picker feedback.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_categories.search.matchCount', 'zh-CN', '匹配', 200, 'admin', 'product category tree search result count prefix', 1, now(), now()),
('admin.product_categories.search.matchCount', 'en-US', 'Matched', 200, 'admin', 'product category tree search result count prefix', 1, now(), now()),
('admin.product_categories.search.matchSuffix', 'zh-CN', '条；上级路径仅供定位', 200, 'admin', 'product category tree search result count suffix', 1, now(), now()),
('admin.product_categories.search.matchSuffix', 'en-US', 'results; ancestor paths are shown for context only', 200, 'admin', 'product category tree search result count suffix', 1, now(), now()),
('admin.product_categories.search.matched', 'zh-CN', '匹配', 200, 'admin', 'product category tree matched-row badge', 1, now(), now()),
('admin.product_categories.search.matched', 'en-US', 'Match', 200, 'admin', 'product category tree matched-row badge', 1, now(), now()),
('admin.product_categories.search.ancestor', 'zh-CN', '上级路径', 200, 'admin', 'product category tree ancestor context label', 1, now(), now()),
('admin.product_categories.search.ancestor', 'en-US', 'Ancestor path', 200, 'admin', 'product category tree ancestor context label', 1, now(), now()),
('admin.product_categories.children.empty', 'zh-CN', '没有子分类', 200, 'admin', 'product category child fragment empty state', 1, now(), now()),
('admin.product_categories.children.empty', 'en-US', 'No child categories', 200, 'admin', 'product category child fragment empty state', 1, now(), now()),
('admin.product_categories.children.paginationLabel', 'zh-CN', '子分类分页', 200, 'admin', 'product category child fragment pagination label', 1, now(), now()),
('admin.product_categories.children.paginationLabel', 'en-US', 'Child category pagination', 200, 'admin', 'product category child fragment pagination label', 1, now(), now()),
('admin.product_categories.children.pagePrefix', 'zh-CN', '第', 200, 'admin', 'product category child fragment page prefix', 1, now(), now()),
('admin.product_categories.children.pagePrefix', 'en-US', 'Page', 200, 'admin', 'product category child fragment page prefix', 1, now(), now()),
('admin.product_categories.children.pageSuffix', 'zh-CN', '页', 200, 'admin', 'product category child fragment page suffix', 1, now(), now()),
('admin.product_categories.children.pageSuffix', 'en-US', '', 200, 'admin', 'product category child fragment page suffix', 1, now(), now()),
('admin.product_categories.parentSearch.label', 'zh-CN', '搜索父级分类', 200, 'admin', 'product category parent picker search label', 1, now(), now()),
('admin.product_categories.parentSearch.label', 'en-US', 'Search parent category', 200, 'admin', 'product category parent picker search label', 1, now(), now()),
('admin.product_categories.parentSearch.placeholder', 'zh-CN', '按名称搜索父级', 200, 'admin', 'product category parent picker search placeholder', 1, now(), now()),
('admin.product_categories.parentSearch.placeholder', 'en-US', 'Search parents by name', 200, 'admin', 'product category parent picker search placeholder', 1, now(), now()),
('admin.product_categories.parentSearch.empty', 'zh-CN', '没有匹配的分类', 200, 'admin', 'product category parent picker empty result', 1, now(), now()),
('admin.product_categories.parentSearch.empty', 'en-US', 'No matching categories', 200, 'admin', 'product category parent picker empty result', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
