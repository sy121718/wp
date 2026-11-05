-- 会员写操作改为 Jet HTML 结果片段：补齐重算结论的短词条。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('memRecalcDone', 'zh-CN', '已扫描 {scanned} 条归属，重算改动 {changed} 条，跳过 {skipped} 条。', 200, 'membership', 'admin/membership/membership_recalc_result.html', 1, now(), now()),
('memRecalcDone', 'en-US', 'Scanned {scanned} assignments, changed {changed}, skipped {skipped}.', 200, 'membership', 'admin/membership/membership_recalc_result.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
