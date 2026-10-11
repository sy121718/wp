-- 613：翻译工作台的保存回执从「三段词条 + Go 拼串」收成一句带占位符的整句。
--
-- 原形态（192 号迁移落的三条）：
--   saved_lead「已保存 」 + saved_mid「 条译文。」 + 可选 saved_tail「译文变更已标记全站待重建…」
-- 三段由 handler 用 shell.TranslateFor 各取一次词、再用 strconv 拼起来 ——
-- 那是**控制器取词**的最后一个出口：改一句话要动 Go 代码，而英文界面上
-- 「Saved N translations.」与中文「已保存 N 条译文。」的空格与语序差异，三段式拼不出来。
--
-- 现形态：一句整句带 %s，取词在模板（jump.html 的 tf）里做，Go 只交 key + 数字参数。
-- 拆成两条 key 是因为两种结论的整句不同（「已保存 0 条译文。」与
-- 「已保存 3 条译文。译文变更已标记全站待重建（下次构建生效）。」），
-- 而第二句不是第一句加后缀 —— 英文里时态与主语都变（"The changes marked …"）。
--
-- 旧三条一并 DELETE：留着就是表里两份真源，谁读哪一份取决于调用点，静默漂移
-- （AGENTS.md §数据库「一张表只有一个模块读写它」的同一条理由）。
--
-- 列清单与 192/612 同形：(item_key, lang, item_value, http_code, category, remark,
-- status, create_time, update_time)。remark 里写「谁在用」而不是抄一遍文案。

DELETE FROM sys_i18n
 WHERE item_key IN ('admin.page_translations.saved_lead',
                    'admin.page_translations.saved_mid',
                    'admin.page_translations.saved_tail');

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.page_translations.saved', 'zh-CN', '已保存 %s 条译文。', 200, 'admin', '翻译工作台保存回执（page_jump.go，jump.html 取词）', 1, now(), now()),
('admin.page_translations.saved', 'en-US', 'Saved %s translations.', 200, 'admin', 'Translation workbench save receipt (page_jump.go)', 1, now(), now()),
('admin.page_translations.saved_rebuild', 'zh-CN', '已保存 %s 条译文。译文变更已标记全站待重建（下次构建生效）。', 200, 'admin', '翻译工作台保存回执 + 全站待重建（page_jump.go）', 1, now(), now()),
('admin.page_translations.saved_rebuild', 'en-US', 'Saved %s translations. The changes marked the whole site for rebuild (they take effect at the next build).', 200, 'admin', 'Translation workbench save receipt with rebuild notice (page_jump.go)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
