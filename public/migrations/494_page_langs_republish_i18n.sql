-- 494 · 语言面板「重新发布」的词条（V4：恢复排除后的显式重发入口）
--
-- 背景：恢复排除只解除限制、**不自动上线**（重发有回执 / 失效 / 回滚语义，不该由勾选框隐式触发）。
-- 但面板上原先没有显式入口，用户取消后译好了只能绕去发布页 —— 本批补一个一次点击的按钮，
-- 它的两个文案（按钮 + 结果回执）必须同批 seed，否则英文界面回落中文。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。
-- 判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.page.langs.action.republish', 'zh-CN', '重新发布', 200, 'admin', 'V4 语言面板：恢复排除后的显式重发按钮', 1, now(), now()),
('admin.page.langs.action.republish', 'en-US', 'Republish', 200, 'admin', 'V4 language panel: explicit republish button', 1, now(), now()),
('admin.page.langs.republished', 'zh-CN', '已重新发布该语言：产物已上线，切换器 / hreflang / sitemap 同步恢复', 200, 'admin', 'V4 重发成功回执', 1, now(), now()),
('admin.page.langs.republished', 'en-US', 'Language republished: artifacts are live again and it is back in the switcher / hreflang / sitemap', 200, 'admin', 'V4 republish success receipt', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
