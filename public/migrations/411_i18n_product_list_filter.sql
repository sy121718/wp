-- ========================================
-- 411 — 商品域 4 个后台列表页补筛选入口与分页（审计 02-M 的 D12 / D13）
--
-- 背景：`product_attributes.html` / `product_categories.html` / `product_brands.html` /
-- `product_tags.html` 四页此前没有任何关键词入口（`keyword` / `filter` / `search` 字样为 0、
-- `.filter-bar` 计数为 0），数据变多后只能肉眼看 + 浏览器 Ctrl+F；其中分类 / 品牌 / 标签
-- 三页还是**全量渲染**（查询不带 Size，页高随数据量无上限），属性页则被 `Size:200`
-- 静默截断（第 201 个属性组消失且页面不给任何提示）。
--
-- 本批的 Go 侧改动：四页的 handler 读 query（关键词 + 回显）、接 `shell.BuildPagination`
-- 与 `partials/pagination.html`，并把空态拆成「筛出来是空的」与「工程里本来就没有」两档。
-- 模板里新增的文案位需要一个词条，否则只能硬编码中文（界面语言切换时不会跟着变）。
--
-- 词条分两类：
--   · `admin.common.filter.*` —— 四页筛选栏与空态共用的通用文案（提交 / 重置 / 全部分支 / 空态提示）。
--     放进 `admin.common.*` 而不是各页各写一份：这四页的筛选栏结构完全相同，同一句「重置」
--     抄四份的后果是改措辞时只改一处，另外三处静默不一致。
--   · `admin.<页面>.filter.phKeyword` —— 每页关键词输入框的 placeholder 不同（属性组按
--     名称 / 标识，分类 / 品牌 / 标签按名称 / URL 段），无法共享；
--     `admin.<页面>.(list.)emptyFilteredTitle` —— 「筛出来是空的」时的空态标题。标题不能复用
--     原有的「还没有 X」：分类存在、只是被筛掉了，那句会误导用户以为数据没了。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**只新增、不修改任何既有词条**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.common.filter.submit', 'zh-CN', '筛选', 200, 'admin', '通用：筛选栏的提交按钮（商品域四个子列表页共用）', 1, now(), now()),
('admin.common.filter.submit', 'en-US', 'Filter', 200, 'admin', '通用：筛选栏的提交按钮（商品域四个子列表页共用）', 1, now(), now()),
('admin.common.filter.reset', 'zh-CN', '重置', 200, 'admin', '通用：清掉全部筛选条件（筛选栏与「筛出来是空的」空态共用）', 1, now(), now()),
('admin.common.filter.reset', 'en-US', 'Reset', 200, 'admin', '通用：清掉全部筛选条件（筛选栏与「筛出来是空的」空态共用）', 1, now(), now()),
('admin.common.filter.optionAll', 'zh-CN', '（全部）', 200, 'admin', '通用：下拉筛选项的「不过滤」分支', 1, now(), now()),
('admin.common.filter.optionAll', 'en-US', '(All)', 200, 'admin', '通用：下拉筛选项的「不过滤」分支', 1, now(), now()),
('admin.common.filter.emptyHint', 'zh-CN', '换个关键词再试，或清掉筛选看全部。', 200, 'admin', '通用：筛选无结果时的空态描述（与 .empty-actions 里的重置按钮同现）', 1, now(), now()),
('admin.common.filter.emptyHint', 'en-US', 'Try another keyword, or clear the filter to see everything.', 200, 'admin', '通用：筛选无结果时的空态描述（与 .empty-actions 里的重置按钮同现）', 1, now(), now()),
('admin.product_attributes.filter.phKeyword', 'zh-CN', '名称 / 标识', 200, 'admin', 'admin/product_attributes.html: 关键词输入框占位（服务端按 name / key 子串匹配）', 1, now(), now()),
('admin.product_attributes.filter.phKeyword', 'en-US', 'Name / key', 200, 'admin', 'admin/product_attributes.html: 关键词输入框占位（服务端按 name / key 子串匹配）', 1, now(), now()),
('admin.product_attributes.list.emptyFilteredTitle', 'zh-CN', '没有匹配的属性组', 200, 'admin', 'admin/product_attributes.html: 筛选无结果时的空态标题（区别于此前的「还没有属性组」）', 1, now(), now()),
('admin.product_attributes.list.emptyFilteredTitle', 'en-US', 'No matching attribute groups', 200, 'admin', 'admin/product_attributes.html: 筛选无结果时的空态标题（区别于此前的「还没有属性组」）', 1, now(), now()),
('admin.product_categories.filter.phKeyword', 'zh-CN', '分类名 / URL 段', 200, 'admin', 'admin/product_categories.html: 关键词输入框占位（服务端按 name 子串匹配）', 1, now(), now()),
('admin.product_categories.filter.phKeyword', 'en-US', 'Category name / slug', 200, 'admin', 'admin/product_categories.html: 关键词输入框占位（服务端按 name 子串匹配）', 1, now(), now()),
('admin.product_categories.empty.filteredTitle', 'zh-CN', '没有匹配的分类', 200, 'admin', 'admin/product_categories.html: 筛选无结果时的空态标题（区别于此前的「还没有分类」）', 1, now(), now()),
('admin.product_categories.empty.filteredTitle', 'en-US', 'No matching categories', 200, 'admin', 'admin/product_categories.html: 筛选无结果时的空态标题（区别于此前的「还没有分类」）', 1, now(), now()),
('admin.product_brands.filter.phKeyword', 'zh-CN', '品牌名 / URL 段', 200, 'admin', 'admin/product_brands.html: 关键词输入框占位（服务端按 name 子串匹配）', 1, now(), now()),
('admin.product_brands.filter.phKeyword', 'en-US', 'Brand name / slug', 200, 'admin', 'admin/product_brands.html: 关键词输入框占位（服务端按 name 子串匹配）', 1, now(), now()),
('admin.product_brands.empty.filteredTitle', 'zh-CN', '没有匹配的品牌', 200, 'admin', 'admin/product_brands.html: 筛选无结果时的空态标题（区别于此前的「还没有品牌」）', 1, now(), now()),
('admin.product_brands.empty.filteredTitle', 'en-US', 'No matching brands', 200, 'admin', 'admin/product_brands.html: 筛选无结果时的空态标题（区别于此前的「还没有品牌」）', 1, now(), now()),
('admin.product_tags.filter.phKeyword', 'zh-CN', '标签名 / URL 段', 200, 'admin', 'admin/product_tags.html: 关键词输入框占位（服务端按 name 子串匹配）', 1, now(), now()),
('admin.product_tags.filter.phKeyword', 'en-US', 'Tag name / slug', 200, 'admin', 'admin/product_tags.html: 关键词输入框占位（服务端按 name 子串匹配）', 1, now(), now()),
('admin.product_tags.list.emptyFilteredTitle', 'zh-CN', '没有匹配的标签', 200, 'admin', 'admin/product_tags.html: 筛选无结果时的空态标题（区别于此前的「还没有标签」）', 1, now(), now()),
('admin.product_tags.list.emptyFilteredTitle', 'en-US', 'No matching tags', 200, 'admin', 'admin/product_tags.html: 筛选无结果时的空态标题（区别于此前的「还没有标签」）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
