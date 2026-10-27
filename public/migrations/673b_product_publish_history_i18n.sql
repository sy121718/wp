-- 673b · 发布历史（迁移 673 / B11）的文案词条。
--
--   台账 presentation_publish_attempts 记的是「谁在什么时候因为什么发了、或没发出去」。
--   页面把它们读出来展示，所以十六种状态的词都落在 Jet 模板的 <template> 里；
--   本迁移只 seed 这 15 个 key（中英各一行，共 30 行）。
--
--   触发源七档与 673 的 ck_publish_attempts_trigger 逐字对应：多一档少一档都会让某次尝试
--   在页面上显示成空白（data-when 全不命中）。
--
-- 幂等：全部新增 key，DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.publish.history.title', 'zh-CN', '发布历史', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.title', 'en-US', 'Publish history', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.lead', 'zh-CN', '每一次发布尝试都记在这里，含自动重建与失败：失败会留下原因原文。默认显示最近 10 次。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.lead', 'en-US', 'Every publish attempt is recorded here, including automatic rebuilds and failures: a failure keeps the original error text. The latest 10 are shown by default.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.hint', 'zh-CN', '行内「发布历史」列出这个商品的每一次发布尝试（含自动重建与失败），默认最近 10 次，可加载更多。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.hint', 'en-US', 'The in-row Publish history button lists every publish attempt for this product, including automatic rebuilds and failures; the latest 10 are shown first and more can be loaded.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.ok', 'zh-CN', '成功', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.ok', 'en-US', 'Succeeded', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.failed', 'zh-CN', '失败', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.failed', 'en-US', 'Failed', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.trigger', 'zh-CN', '触发', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.trigger', 'en-US', 'Trigger', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.create', 'zh-CN', '首次发布', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.create', 'en-US', 'First publish', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.manual', 'zh-CN', '手动发布', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.manual', 'en-US', 'Manual publish', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.switch', 'zh-CN', '换模板', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.switch', 'en-US', 'Template switch', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.url', 'zh-CN', '改 URL', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.url', 'en-US', 'URL change', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.rollback', 'zh-CN', '回滚', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.rollback', 'en-US', 'Rollback', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.override', 'zh-CN', '改文档', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.override', 'en-US', 'Document edit', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.auto', 'zh-CN', '自动重建', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.t.auto', 'en-US', 'Auto rebuild', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.more', 'zh-CN', '加载更多', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.more', 'en-US', 'Load more', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.empty', 'zh-CN', '这个商品还没有发布尝试记录。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.history.empty', 'en-US', 'No publish attempts recorded for this product yet.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
