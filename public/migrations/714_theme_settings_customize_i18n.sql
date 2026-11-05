-- 714 · 主题设置「块自定义」按钮的两条界面词条
--
-- 为什么单独一批：`internal/templates/admin/project/theme_settings.html` 的页眉 / 页脚
--   块选择器旁有一个「自定义」链接（选中块时可用）和一个同样的按钮（未选块时禁用，
--   title 提示先选块）。这两句话从来没有词条，`scripts/check-i18n-keys-seeded.sh`
--   判它们「被引用但未 seed」，英文界面下只能看到中文兜底。
--
-- 用法位置（模板里共 9 处引用，三个槽位：页眉块 / 页脚块 / 公告条）：
--   · `admin.theme_settings.customize`        —— 链接 / 按钮文案（:127 :129 / :143 :145 / :163 :165）
--   · `admin.theme_settings.customize_empty`  —— 未选块时的 title 提示，同时作 data 属性（:125 / :141 / :161）
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见 register_theme_settings_customize_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.theme_settings.customize', 'zh-CN', '自定义', 200, 'ui', 'admin/project/theme_settings.html: 页眉 / 页脚块旁的「自定义」链接与按钮', 1, now(), now()),
('admin.theme_settings.customize', 'en-US', 'Customize', 200, 'ui', 'admin/project/theme_settings.html: the Customize link / button next to the header and footer block pickers', 1, now(), now()),
('admin.theme_settings.customize_empty', 'zh-CN', '先选一个块再自定义', 200, 'ui', 'admin/project/theme_settings.html: 未选块时禁用按钮的 title 与 data 属性', 1, now(), now()),
('admin.theme_settings.customize_empty', 'en-US', 'Select a block first to customize it', 200, 'ui', 'admin/project/theme_settings.html: title and data attribute on the disabled button when no block is selected', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
