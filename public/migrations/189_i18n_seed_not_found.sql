-- 189 · 自定义 404 页的后台文案（审计 SEO-013 的配套词条）。
--
-- 这些 key 由 SEO-013 的后台表单项引入（SiteSettings.NotFoundHTML），
-- 但当时没有同批 seed —— 表现是英文界面下这 9 处回落中文兜底。
-- 与 187 / 188 同一形状与判据口径：按本批自己的 key 枚举计数，不用总量。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('admin.settings.field.not_found_html', 'zh-CN', '自定义 404 页（HTML）', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.field.not_found_html', 'en-US', 'Custom 404 page (HTML)', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.ph.not_found_html', 'zh-CN', '留空即不配置。粘贴一份完整的 404 页面 HTML，例如以 <!doctype html> 开头。', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.ph.not_found_html', 'en-US', 'Leave blank to keep the default. Paste a complete 404 page HTML document, e.g. starting with <!doctype html>.', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.lead', 'zh-CN', '留空即不配置：线上仍是默认的 404 提示。填了之后，访客访问不存在的路径看到的就是这份页面，', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.lead', 'en-US', 'Leave it blank to keep the built-in 404 notice. Once filled in, visitors hitting a missing path see this page,', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.strong', 'zh-CN', '状态码仍然是 404', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.strong', 'en-US', 'the status code is still 404', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.after', 'zh-CN', '（不是 200 软 404：搜索引擎不会把死链当成有效页面收录）。内容是一份完整 HTML 文档，发布时原样写到站点根的', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.after', 'en-US', '(not a soft 200: search engines will not index dead links as valid pages). The content is a complete HTML document, written as-is to', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.tail', 'zh-CN', '，上限 32 KiB。', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.tail', 'en-US', 'the site root on publish, up to 32 KiB.', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.republish_lead', 'zh-CN', '保存后', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.republish_lead', 'en-US', 'After you save,', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.republish', 'zh-CN', '需要重新发布页面', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.republish', 'en-US', 'you must republish the pages', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.republish_tail', 'zh-CN', '才会生效：它是静态文件，与 sitemap / feed 一起在发布时刷新；改动影响全站所有错误路径。', 'admin', '', 1, 200, now(), now()),
    ('admin.settings.hint.not_found_html.republish_tail', 'en-US', 'for it to take effect. It is a static file, refreshed at publish time together with sitemap and feed, and any change affects every error path on the site.', 'admin', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
