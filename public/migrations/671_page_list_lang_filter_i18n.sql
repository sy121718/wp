-- 671 · 页面列表语言维度筛选的词条（批2：?lang=&state=）
--
-- 背景：页面列表原先只能按站点工程看，运营问「哪些页面还没出日语」时没有地方回答。
-- 本批给列表加第二条轴（语言 × 该语言的产出状态），这些文案必须同批 seed，
-- 否则英文界面回落中文：
--   · filter_lang / filter_lang_all / filter_lang_state —— 筛选栏的两个下拉与「全部语言」项
--   · empty_lang_title / empty_lang_desc —— 筛了语言却为空时的第三档空态
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。
-- 判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.pages.filter_lang', 'zh-CN', '语言', 200, 'admin', '批2 页面列表：语言维度筛选', 1, now(), now()),
('admin.pages.filter_lang', 'en-US', 'Language', 200, 'admin', 'Batch 2 page list: language dimension filter', 1, now(), now()),
('admin.pages.filter_lang_all', 'zh-CN', '全部语言', 200, 'admin', '批2 页面列表：不按语言筛选', 1, now(), now()),
('admin.pages.filter_lang_all', 'en-US', 'All languages', 200, 'admin', 'Batch 2 page list: no language filter', 1, now(), now()),
('admin.pages.filter_lang_state', 'zh-CN', '产出状态', 200, 'admin', '批2 页面列表：该语言的产出状态', 1, now(), now()),
('admin.pages.filter_lang_state', 'en-US', 'Output status', 200, 'admin', 'Batch 2 page list: output status of that language', 1, now(), now()),
('admin.pages.empty_lang_title', 'zh-CN', '这个语言下没有符合状态的页面', 200, 'admin', '批2 页面列表：按语言筛空的标题', 1, now(), now()),
('admin.pages.empty_lang_title', 'en-US', 'No page matches this status in that language', 200, 'admin', 'Batch 2 page list: empty state title for language filter', 1, now(), now()),
('admin.pages.empty_lang_desc', 'zh-CN', '换一个「产出状态」看（新语言的页面通常还在「未发布」），或换一个语言；要让某页始终产出这门语言，去该页的「多语言」工作台调整语言产出范围。', 200, 'admin', '批2 页面列表：按语言筛空的下一步', 1, now(), now()),
('admin.pages.empty_lang_desc', 'en-US', 'Try another output status (pages in a new language are usually still unpublished) or another language. To always produce a language for a page, adjust its language scope in that page’s translations workbench.', 200, 'admin', 'Batch 2 page list: next step when language filter is empty', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
