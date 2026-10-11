-- 612 · 客户列表筛选的归口文案 + 商品评分参数级提示（6 个 key，中英成对）。
--
-- 背景：这批 key 此前只以「事实」（key + 中文兜底）存在于 Go 侧，词条从未 seed。
-- 后果不是异常，而是**安静地少了一种语言**：
--   · 缺词条 → 取词回落 Go 侧的 Fallback，中文环境下**完全看不出来**；
--   · 只有切到 en-US 才暴露，而那时看到的是中文句子。
-- 门禁 scripts/check-i18n-keys-seeded.sh 的判据是「key 被引用但没有任何
-- INSERT INTO sys_i18n 元组」，本批把这 6 条补齐（该门禁基线为 0，只降不升）。
--
-- 六条各自的来处：
--   · admin.customer.filter.rfm_unavailable      RFM 分段筛选整维失败（数据没接上）
--   · admin.customer.filter.segment_unavailable  消费分段筛选整维失败（数据没接上）
--   · admin.customer.filter.tier_truncated       会员等级维度人数超过翻页上限，整维当失败
--   · admin.customer.filter.tier_unavailable     会员等级筛选不可用（会员模块没接上）
--   · admin.customer.rfm.unavailable             RFM 明细页取数不可用
--   · admin.products.rating.invalid              补录评分时参数不是 0~5 的数字
--
-- 为什么「整维失败」要明说而不是给一半 id：交出一半 id 会让页面显示一个看起来正常、
-- 只是少了一批人的列表，而用户无从知道少了 —— 这两条 key 是那条判据的用户可见面。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（不覆盖运营改过的翻译）。
-- 门槛判据在 register 里**逐条枚举**本批的 6 个 item_key × 两种语言（上界封闭）——
-- 前缀 LIKE 会被同期其它批次的行满足而静默跳过，全库总量会让本批每次启动重跑
--（两种都踩过，见 226/277/283/592）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customer.filter.rfm_unavailable', 'zh-CN', '按 RFM 分段筛选暂时不可用（数据没接上），请先用其它条件。', 200, 'admin', '客户列表 RFM 分段维度整维失败（user_page.go）', 1, now(), now()),
('admin.customer.filter.rfm_unavailable', 'en-US', 'RFM segment filtering is unavailable right now (data not connected). Use other filters.', 200, 'admin', 'Customer list: RFM segment dimension failed (user_page.go)', 1, now(), now()),
('admin.customer.filter.segment_unavailable', 'zh-CN', '按消费分段筛选暂时不可用（数据没接上），请先用其它条件。', 200, 'admin', '客户列表消费分段维度整维失败（user_page.go）', 1, now(), now()),
('admin.customer.filter.segment_unavailable', 'en-US', 'Spending segment filtering is unavailable right now (data not connected). Use other filters.', 200, 'admin', 'Customer list: spending segment dimension failed (user_page.go)', 1, now(), now()),
('admin.customer.filter.tier_truncated', 'zh-CN', '该会员等级的人数超过筛选上限，无法作为筛选项使用。请配合其它条件一起筛。', 200, 'admin', '客户列表会员等级维度达到翻页上限后整维作失败（user_page.go）', 1, now(), now()),
('admin.customer.filter.tier_truncated', 'en-US', 'This membership tier exceeds the filtering limit and cannot be used as a filter. Narrow it with other conditions.', 200, 'admin', 'Customer list: tier dimension hit the paging cap, whole dimension fails (user_page.go)', 1, now(), now()),
('admin.customer.filter.tier_unavailable', 'zh-CN', '按会员等级筛选暂时不可用（会员模块没接上），请先用其它条件。', 200, 'admin', '客户列表会员等级维度整维失败（user_page.go）', 1, now(), now()),
('admin.customer.filter.tier_unavailable', 'en-US', 'Membership tier filtering is unavailable right now. Use other filters.', 200, 'admin', 'Customer list: membership tier dimension failed (user_page.go)', 1, now(), now()),
('admin.customer.rfm.unavailable', 'zh-CN', 'RFM 分析暂时不可用（数据没接上），请稍后再试。', 200, 'admin', '客户 RFM 明细取数不可用（user_page.go）', 1, now(), now()),
('admin.customer.rfm.unavailable', 'en-US', 'RFM analysis is unavailable right now (data not connected). Try again later.', 200, 'admin', 'Customer RFM detail: data unavailable (user_page.go)', 1, now(), now()),
('admin.products.rating.invalid', 'zh-CN', '评分必须是 0~5 的数字。', 200, 'admin', '商品评分补录的参数级校验提示（product_page.go）', 1, now(), now()),
('admin.products.rating.invalid', 'en-US', 'Rating must be a number between 0 and 5.', 200, 'admin', 'Product rating: parameter validation message (product_page.go)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
