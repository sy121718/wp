-- 商品定价预览的汇总结论：模板已在取词，词库缺中英两行（具名占位参数）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('prPrevSummary', 'zh-CN', '参与 {target} 个变体（改价 {changed} / 价格不变 {same} / 跳过 {skipped}）', 200, 'product', 'admin/product/pricing_preview_result.html: 预览汇总', 1, now(), now()),
('prPrevSummary', 'en-US', '{target} variants affected (price changed {changed} / unchanged {same} / skipped {skipped})', 200, 'product', 'admin/product/pricing_preview_result.html: 预览汇总', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
