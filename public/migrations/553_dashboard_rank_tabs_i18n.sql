-- 553 · 概览页排行榜双 Tab 与页面类型的词条（admin/dashboard.html）
--
-- 背景：榜单拆成两个 Tab —— 「热销商品」与「热门页面」（页面前十）。两张表
--       服务端都渲染好，切换纯前端。
--
-- 覆盖：13 个新 key × 2 语言 = 26 条（ON CONFLICT DO NOTHING），
--       另加 top.title × 2 语言的文案覆盖（卡片刻度从「热销商品」扩到两个榜，改文案不换 key）。
--
-- pageKind.* 六条是「页面类型 → 文案」的映射（Go 侧 overviewPageKindKey 给 key、
-- 词条给文案）：模板里的取值是动态 key，静态扫描看不到它们，但英文界面照样需要，
-- 所以它们必须在这里 seed。
--
-- 不新增 rank.title：卡片标题仍用 top.title（语义就是「榜单标题」），
-- 改名文案不换钥匙 —— 换钥匙会留下一条再也没人引用的 top.title，
-- 而「模板取词必须有 seed」那条门禁只会发现缺的、不会发现多的。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.rank.empty', 'en-US', 'Nothing to rank in this range', 200, 'admin', 'admin/dashboard.html: 两个榜单都无数据时的空态', 1, now(), now()),
('admin.dashboard.rank.empty', 'zh-CN', '这段时间没有可排行的数据', 200, 'admin', 'admin/dashboard.html: 两个榜单都无数据时的空态', 1, now(), now()),
('admin.dashboard.rank.tabs', 'en-US', 'Ranking metric', 200, 'admin', 'admin/dashboard.html: 排行榜 Tab 组无障碍标签', 1, now(), now()),
('admin.dashboard.rank.tabs', 'zh-CN', '排行维度', 200, 'admin', 'admin/dashboard.html: 排行榜 Tab 组无障碍标签', 1, now(), now()),
('admin.dashboard.rank.tabProducts', 'en-US', 'Top products', 200, 'admin', 'admin/dashboard.html: 排行榜 Tab：热销商品', 1, now(), now()),
('admin.dashboard.rank.tabProducts', 'zh-CN', '热销商品', 200, 'admin', 'admin/dashboard.html: 排行榜 Tab：热销商品', 1, now(), now()),
('admin.dashboard.rank.tabPages', 'en-US', 'Top pages', 200, 'admin', 'admin/dashboard.html: 排行榜 Tab：热门页面', 1, now(), now()),
('admin.dashboard.rank.tabPages', 'zh-CN', '热门页面', 200, 'admin', 'admin/dashboard.html: 排行榜 Tab：热门页面', 1, now(), now()),
('admin.dashboard.pages.empty', 'en-US', 'No page views in this range', 200, 'admin', 'admin/dashboard.html: 页面排行无数据时的空态', 1, now(), now()),
('admin.dashboard.pages.empty', 'zh-CN', '这段时间没有访问记录', 200, 'admin', 'admin/dashboard.html: 页面排行无数据时的空态', 1, now(), now()),
('admin.dashboard.pages.col.path', 'en-US', 'Path', 200, 'admin', 'admin/dashboard.html: 页面排行表头（路径）', 1, now(), now()),
('admin.dashboard.pages.col.path', 'zh-CN', '路径', 200, 'admin', 'admin/dashboard.html: 页面排行表头（路径）', 1, now(), now()),
('admin.dashboard.pages.col.views', 'en-US', 'Views', 200, 'admin', 'admin/dashboard.html: 页面排行表头（浏览量）', 1, now(), now()),
('admin.dashboard.pages.col.views', 'zh-CN', '浏览量', 200, 'admin', 'admin/dashboard.html: 页面排行表头（浏览量）', 1, now(), now()),
('admin.dashboard.pageKind.home', 'en-US', 'Home', 200, 'admin', 'admin/dashboard.html: 页面类型标签（首页）', 1, now(), now()),
('admin.dashboard.pageKind.home', 'zh-CN', '首页', 200, 'admin', 'admin/dashboard.html: 页面类型标签（首页）', 1, now(), now()),
('admin.dashboard.pageKind.page', 'en-US', 'Page', 200, 'admin', 'admin/dashboard.html: 页面类型标签（单页）', 1, now(), now()),
('admin.dashboard.pageKind.page', 'zh-CN', '单页', 200, 'admin', 'admin/dashboard.html: 页面类型标签（单页）', 1, now(), now()),
('admin.dashboard.pageKind.article', 'en-US', 'Article', 200, 'admin', 'admin/dashboard.html: 页面类型标签（文章）', 1, now(), now()),
('admin.dashboard.pageKind.article', 'zh-CN', '文章', 200, 'admin', 'admin/dashboard.html: 页面类型标签（文章）', 1, now(), now()),
('admin.dashboard.pageKind.tag', 'en-US', 'Tag', 200, 'admin', 'admin/dashboard.html: 页面类型标签（标签页）', 1, now(), now()),
('admin.dashboard.pageKind.tag', 'zh-CN', '标签页', 200, 'admin', 'admin/dashboard.html: 页面类型标签（标签页）', 1, now(), now()),
('admin.dashboard.pageKind.archive', 'en-US', 'Archive', 200, 'admin', 'admin/dashboard.html: 页面类型标签（归档页）', 1, now(), now()),
('admin.dashboard.pageKind.archive', 'zh-CN', '归档页', 200, 'admin', 'admin/dashboard.html: 页面类型标签（归档页）', 1, now(), now()),
('admin.dashboard.pageKind.search', 'en-US', 'Search', 200, 'admin', 'admin/dashboard.html: 页面类型标签（搜索结果）', 1, now(), now()),
('admin.dashboard.pageKind.search', 'zh-CN', '搜索结果', 200, 'admin', 'admin/dashboard.html: 页面类型标签（搜索结果）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 卡片标题：从「热销商品（当前区间）」改成「排行榜（当前区间）」（加 Tab 后这张卡不再只放商品榜）。
UPDATE sys_i18n SET item_value = 'Rankings (this range)', update_time = now()
WHERE item_key = 'admin.dashboard.top.title' AND lang = 'en-US' AND item_value <> 'Rankings (this range)';
UPDATE sys_i18n SET item_value = '排行榜（当前区间）', update_time = now()
WHERE item_key = 'admin.dashboard.top.title' AND lang = 'zh-CN' AND item_value <> '排行榜（当前区间）';
