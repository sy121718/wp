-- 495 · 页面列表页「缺译报告」入口按钮的词条（W4 的入口）
--
-- 背景：缺译报告页（U2）此前只能手输 URL 到达。本批补上入口按钮，它的文案必须同批 seed，
-- 否则英文界面回落中文（模板里的中文只是 t() 兜底）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.pages.action.translation_misses', 'zh-CN', '缺译报告', 200, 'admin', '页面列表页入口：内容缺译报告（U2）', 1, now(), now()),
('admin.pages.action.translation_misses', 'en-US', 'Missing translations', 200, 'admin', 'Pages list entry: missing content translations report (U2)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
