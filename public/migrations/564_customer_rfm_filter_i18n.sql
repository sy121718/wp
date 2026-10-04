-- 564 · 客户列表「RFM 分段」筛选的词条（admin/user/customers.html）
--
-- 背景：客户列表已有消费分段（新客 / 回头客 / 复购）与复购次数，本批补上 RFM 分段。
--       两者是**两套不同的分段**（一个看下单行为、一个看 RFM 总分），同时选时取交集。
--
-- 覆盖：6 个新 key × 2 语言 = 12 条。
--
-- 「三项分数都是相对分」必须留在页面上：五分位换一个时间段就会变，
-- 不写的话分段名（高价值）一定会被当成绝对等级。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customers.field.rfm', 'en-US', 'RFM segment', 200, 'admin', 'admin/user/customers.html: 筛选标签（RFM 分段）', 1, now(), now()),
('admin.customers.field.rfm', 'zh-CN', 'RFM 分段', 200, 'admin', 'admin/user/customers.html: 筛选标签（RFM 分段）', 1, now(), now()),
('admin.customers.rfm.all', 'en-US', 'Any segment', 200, 'admin', 'admin/user/customers.html: RFM 选项（不限）', 1, now(), now()),
('admin.customers.rfm.all', 'zh-CN', '不限分段', 200, 'admin', 'admin/user/customers.html: RFM 选项（不限）', 1, now(), now()),
('admin.customers.rfm.vip', 'en-US', 'High value', 200, 'admin', 'admin/user/customers.html: RFM 选项（高价值）', 1, now(), now()),
('admin.customers.rfm.vip', 'zh-CN', '高价值', 200, 'admin', 'admin/user/customers.html: RFM 选项（高价值）', 1, now(), now()),
('admin.customers.rfm.potential', 'en-US', 'Potential', 200, 'admin', 'admin/user/customers.html: RFM 选项（潜力）', 1, now(), now()),
('admin.customers.rfm.potential', 'zh-CN', '潜力', 200, 'admin', 'admin/user/customers.html: RFM 选项（潜力）', 1, now(), now()),
('admin.customers.rfm.low_value', 'en-US', 'General', 200, 'admin', 'admin/user/customers.html: RFM 选项（一般）', 1, now(), now()),
('admin.customers.rfm.low_value', 'zh-CN', '一般', 200, 'admin', 'admin/user/customers.html: RFM 选项（一般）', 1, now(), now()),
('admin.customers.rfm.note', 'en-US', 'Segments come from the RFM total score in the selected window (12+ high value, 8+ potential); all three scores are relative, so they change with the window. Combined with the spend segment above, both conditions must hold.', 200, 'admin', 'admin/user/customers.html: RFM 筛选说明', 1, now(), now()),
('admin.customers.rfm.note', 'zh-CN', '分段按所选时间段内的 RFM 总分划分（≥12 高价值 / ≥8 潜力）；三项分数都是相对分，换个时间段会变。与上面的「消费分段」同时选时两个条件都要成立。', 200, 'admin', 'admin/user/customers.html: RFM 筛选说明', 1, now(), now());
