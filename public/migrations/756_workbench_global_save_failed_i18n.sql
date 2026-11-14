-- 756 · 工作台全局主题面板的保存失败提示。
--
-- 面板保存改为走共享写基座（HTMX + data-msg-failed）：失败时由基座把这句话
-- 写进宿主的状态行，而不是替换正在编辑的字段。
--
-- 幂等：只补缺失的 (item_key, lang)，不覆盖运营已修改的译文。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('workbench.ui.settings.saveFailed', 'zh-CN', '主题设置未保存，请检查输入值。', 200, 'ui', 'global settings panel: save failed', 1, now(), now()),
    ('workbench.ui.settings.saveFailed', 'en-US', 'The theme settings were not saved; please check the values you entered.', 200, 'ui', 'global settings panel: save failed', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
