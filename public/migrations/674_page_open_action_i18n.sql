-- 674 · 页面列表操作列合并为一个「打开页面」入口后的词条。
--
-- 背景（2026-10 用户拍板）：「预览和线上两个按钮也冲突了……有个打开新页面看页面内容的就行了」。
-- 原操作列在同一行并排放「预览」（草稿渲染 /workbench/preview）与「线上」（/site<path>/ 静态产物），
-- 两个按钮回答的是同一个问题（这页现在长什么样），差别只在「看草稿还是看线上」——
-- 而这一差别状态列已经写了（已发布 / 已暂存待发布 / 草稿）。于是并排两个按钮的实际效果是
-- 让用户先猜哪个才对应自己想看的那个。改成单入口：已发布 → 线上真页面，其余 → 草稿渲染。
--
-- 本迁移只补新 key（中英各一行）。192 seed 的 admin.pages.action.preview / .live 保留不动：
-- seed 是默认值来源，历史迁移不回溯改写；它们变成未使用的存量词条，不影响任何界面。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.pages.action.open', 'zh-CN', '打开页面', 200, 'admin', '页面列表：已发布看线上、未发布看草稿', 1, now(), now()),
('admin.pages.action.open', 'en-US', 'Open page', 200, 'admin', 'Page list: live page when published, draft render otherwise', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
