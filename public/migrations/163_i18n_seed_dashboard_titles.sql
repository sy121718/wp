-- 163 · i18n 词条 seed（后台页面标题 + 面包屑 + url_mode 提示，I18N-005 / SEO-010 / I18N-016）
--
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('MsgArticlesTitle', 'zh-CN', '文章', 200, 'ui', 'internal/module/dashboard/inbound/http/article_handle.go', 1, now(), now()),
('MsgArticlesTitle', 'en-US', 'Articles', 200, 'ui', 'internal/module/dashboard/inbound/http/article_handle.go', 1, now(), now()),
('MsgArticlesEditTitle', 'zh-CN', '编辑文章', 200, 'ui', 'internal/module/dashboard/inbound/http/article_handle.go', 1, now(), now()),
('MsgArticlesEditTitle', 'en-US', 'Edit article', 200, 'ui', 'internal/module/dashboard/inbound/http/article_handle.go', 1, now(), now()),
('MsgProductsTitle', 'zh-CN', '商品', 200, 'ui', 'internal/module/dashboard/inbound/http/product_handle.go', 1, now(), now()),
('MsgProductsTitle', 'en-US', 'Products', 200, 'ui', 'internal/module/dashboard/inbound/http/product_handle.go', 1, now(), now()),
('site.breadcrumb.home', 'zh-CN', '首页', 200, 'ui', 'internal/builder/seo_head.go', 1, now(), now()),
('site.breadcrumb.home', 'en-US', 'Home', 200, 'ui', 'internal/builder/seo_head.go', 1, now(), now()),
('MsgSiteLangURLOffWarning', 'zh-CN', '当前语言 URL 方案为 off：各语言映射到同一路径，语言切换器不会渲染；如需多语言独立 URL，请将 i18n.site_lang_url_mode 设为 default_plain 或 all_prefix', 200, 'ui', 'internal/templates/admin/settings.html', 1, now(), now()),
('MsgSiteLangURLOffWarning', 'en-US', 'Language URL mode is off: all locales share the same path, so the language switcher will not render. Set i18n.site_lang_url_mode to default_plain or all_prefix for separate locale URLs.', 200, 'ui', 'internal/templates/admin/settings.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET
  item_value = EXCLUDED.item_value,
  remark = EXCLUDED.remark,
  update_time = now();
