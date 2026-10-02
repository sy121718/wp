-- 502 · i18n 页「新建词条」下拉的语言标记词条（Y3 收编的尾巴）。
--
-- 背景：Y3 把「新建词条」的语言下拉从两条硬编码选项改成读 sys_dict，并在选项后显示
--   「界面已有译文」标记（sys_dict.ui_available）—— 它直接回答运营的下一步
--   「我该给哪个语言补词条」。模板里的中文只是 t() 兜底，词条没 seed 时英文界面回落中文
--   （门禁 admin_group_f_i18n_test.go 的 TestGroupFI18nTemplateKeysMatchSeed 判的就是这条）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.i18n.option.ui_available', 'zh-CN', '（界面已有译文）', 200, 'admin', 'i18n 新建词条下拉：该语言的界面文案已有译文', 1, now(), now()),
('admin.i18n.option.ui_available', 'en-US', '(UI translated)', 200, 'admin', 'i18n new-entry dropdown: this language already has UI translations', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
