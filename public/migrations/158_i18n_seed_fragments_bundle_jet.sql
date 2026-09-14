-- 158 · i18n seed（捆绑配置器 jet 文案 site.fragment.bundle.*，I18N-012 补全）
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.fragment.bundle.qty_aria', 'zh-CN', '数量', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.qty_aria', 'en-US', 'Quantity', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.price_prefix', 'zh-CN', '套餐价', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.price_prefix', 'en-US', 'Bundle price', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.submit', 'zh-CN', '确认数量', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.submit', 'en-US', 'Confirm quantities', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.option_meta', 'zh-CN', '单件 %d ~ %s · 可用 %d', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.option_meta', 'en-US', 'Per item %d ~ %s · %d available', 200, 'ui', 'fragments/bundle_configurator.jet', 1, now(), now()),
('site.fragment.bundle.result_summary', 'zh-CN', '%s · 套餐价 %s · 共 %d 件', 200, 'ui', 'fragments/bundle_configurator_result.jet', 1, now(), now()),
('site.fragment.bundle.result_summary', 'en-US', '%s · Bundle price %s · %d items total', 200, 'ui', 'fragments/bundle_configurator_result.jet', 1, now(), now()),
('site.fragment.bundle.result_item', 'zh-CN', '%s · %s × %d（当前可用 %d）', 200, 'ui', 'fragments/bundle_configurator_result.jet', 1, now(), now()),
('site.fragment.bundle.result_item', 'en-US', '%s · %s × %d (%d available now)', 200, 'ui', 'fragments/bundle_configurator_result.jet', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value, update_time = now();
