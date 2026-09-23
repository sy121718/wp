-- 413 · 内容域四个列表页的筛选栏词条（articles / pages / blocks / navigations）
--
-- 背景（审计 02-L §2 P1-12「缺 .filter-bar」）：这四个列表页此前**没有任何筛选入口** ——
--   articles 一页能到 50 行，数据一多就只能靠浏览器 Ctrl+F。
--   本轮按每页「有什么字段、service 的 List 支持什么维度」逐页判断后补筛选栏：
--     · pages   —— 服务端筛选（工程）：?project= 经 handler 读 query 后进 page Service.ListReq.ProjectID；
--     · articles / blocks / navigations —— 客户端筛选（admin.js 的 [data-filter-input]）：
--       它们的 service 侧没有任何关键词 / 状态维度（content.ListReq 只有 entityType+limit+offset；
--       block/navigation 的 kind/reuseMode 维度已被「三段表 / 位置下拉」占用），
--       为不越界改 service，走项目既有的客户端过滤机制（样板：menus.html / departments.html）。
--   两种机制都需要一句「筛出来是空的」提示（与「本来就没数据」区分）—— 提示文案必须可翻译。
--
-- 为什么必须有一条迁移而不是只改模板：**模板里的中文只是 t() 兜底，词条命中时显示的是库里的值**。
--   seed 一律 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING，是「默认值来源」不是「真相来源」
--   （运营在后台改过的词条不能被部署静默回滚）。与 317 / 399 / 400 同一手法。
--
-- 词条分工：
--   admin.<mod>.filter_*      —— 筛选栏的控件标签与占位符（客户端筛选页另用 filter_placeholder）
--   admin.<mod>.filter_empty  —— 客户端筛选「有词但 0 行可见」的提示（admin.js 的 [data-filter-empty] 契约）
--   admin.pages.empty_project_* —— 服务端筛选（指定了工程）时的空态两档中的第二档
--     （pages 页的默认档继续用既有的 admin.pages.empty.title / admin.pages.empty）
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**只新增、不修改任何既有词条**，故无 UPDATE。
--   判定写法：ConditionSQL 由迁移器 db.Raw 直接执行，没有参数替换，判定用的值只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.article.list.filterLabel', 'zh-CN', '关键词', 200, 'admin', 'admin/articles.html: 客户端筛选栏标签', 1, now(), now()),
('admin.article.list.filterLabel', 'en-US', 'Keyword', 200, 'admin', 'admin/articles.html: 客户端筛选栏标签', 1, now(), now()),
('admin.article.list.filterPlaceholder', 'zh-CN', '标题 / 路径 / 摘要 / 状态', 200, 'admin', 'admin/articles.html: 客户端筛选输入框占位符', 1, now(), now()),
('admin.article.list.filterPlaceholder', 'en-US', 'Title / slug / excerpt / state', 200, 'admin', 'admin/articles.html: 客户端筛选输入框占位符', 1, now(), now()),
('admin.article.list.filterEmpty', 'zh-CN', '没有匹配的文章。换一个关键词，或清空关键词看全部。', 200, 'admin', 'admin/articles.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),
('admin.article.list.filterEmpty', 'en-US', 'No articles match. Try another keyword, or clear it to see them all.', 200, 'admin', 'admin/articles.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),

('admin.pages.filter_project', 'zh-CN', '站点工程', 200, 'admin', 'admin/pages.html: 筛选栏的工程下拉标签', 1, now(), now()),
('admin.pages.filter_project', 'en-US', 'Site project', 200, 'admin', 'admin/pages.html: 筛选栏的工程下拉标签', 1, now(), now()),
('admin.pages.filter_submit', 'zh-CN', '筛选', 200, 'admin', 'admin/pages.html: 筛选栏提交按钮', 1, now(), now()),
('admin.pages.filter_submit', 'en-US', 'Filter', 200, 'admin', 'admin/pages.html: 筛选栏提交按钮', 1, now(), now()),
('admin.pages.filter_reset', 'zh-CN', '重置', 200, 'admin', 'admin/pages.html: 筛选栏重置链接', 1, now(), now()),
('admin.pages.filter_reset', 'en-US', 'Reset', 200, 'admin', 'admin/pages.html: 筛选栏重置链接', 1, now(), now()),
('admin.pages.empty_project_title', 'zh-CN', '这个站点工程还没有页面', 200, 'admin', 'admin/pages.html: 指定工程后无页面的空态标题（与「还没有页面」两档）', 1, now(), now()),
('admin.pages.empty_project_title', 'en-US', 'This site project has no pages yet', 200, 'admin', 'admin/pages.html: 指定工程后无页面的空态标题（与「还没有页面」两档）', 1, now(), now()),
('admin.pages.empty_project_desc', 'zh-CN', '用页头的「创建页面」在这个工程里建一个，或切换上方「站点工程」看别的工程。', 200, 'admin', 'admin/pages.html: 指定工程后无页面的空态引导', 1, now(), now()),
('admin.pages.empty_project_desc', 'en-US', 'Create one in this project from the page header, or switch the site project above.', 200, 'admin', 'admin/pages.html: 指定工程后无页面的空态引导', 1, now(), now()),

('admin.blocks.filter_keyword', 'zh-CN', '关键词', 200, 'admin', 'admin/blocks.html: 客户端筛选栏标签', 1, now(), now()),
('admin.blocks.filter_keyword', 'en-US', 'Keyword', 200, 'admin', 'admin/blocks.html: 客户端筛选栏标签', 1, now(), now()),
('admin.blocks.filter_placeholder', 'zh-CN', '名称 / 类型 / 复用方式', 200, 'admin', 'admin/blocks.html: 客户端筛选输入框占位符', 1, now(), now()),
('admin.blocks.filter_placeholder', 'en-US', 'Name / kind / reuse', 200, 'admin', 'admin/blocks.html: 客户端筛选输入框占位符', 1, now(), now()),
('admin.blocks.filter_empty', 'zh-CN', '没有匹配的块。换一个关键词，或清空关键词看全部。', 200, 'admin', 'admin/blocks.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),
('admin.blocks.filter_empty', 'en-US', 'No blocks match. Try another keyword, or clear it to see them all.', 200, 'admin', 'admin/blocks.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),

('admin.navigations.filter_keyword', 'zh-CN', '关键词', 200, 'admin', 'admin/navigations.html: 客户端筛选栏标签', 1, now(), now()),
('admin.navigations.filter_keyword', 'en-US', 'Keyword', 200, 'admin', 'admin/navigations.html: 客户端筛选栏标签', 1, now(), now()),
('admin.navigations.filter_placeholder', 'zh-CN', '菜单项 / 链接 / 打开方式', 200, 'admin', 'admin/navigations.html: 客户端筛选输入框占位符', 1, now(), now()),
('admin.navigations.filter_placeholder', 'en-US', 'Item / link / target', 200, 'admin', 'admin/navigations.html: 客户端筛选输入框占位符', 1, now(), now()),
('admin.navigations.filter_empty', 'zh-CN', '没有匹配的菜单项。换一个关键词，或清空关键词看全部。', 200, 'admin', 'admin/navigations.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),
('admin.navigations.filter_empty', 'en-US', 'No menu items match. Try another keyword, or clear it to see them all.', 200, 'admin', 'admin/navigations.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
