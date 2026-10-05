-- 565 · 客户列表「会员等级」筛选的词条（admin/user/customers.html）
--
-- 背景：客户列表已有消费分段 / 复购次数 / RFM 分段，本批补上会员等级（第三维）。
--       等级是运营自建的，所以下拉选项由会员模块现场取，只有「不限等级」是固定项。
--
-- 覆盖：3 个新 key × 2 语言 = 6 条。
--
-- 「等级由会员模块评定」必须留在页面上：等级与 RFM 分段看着都像「客户档次」，
-- 不写清来源的话，运营会把两套分档混着解释。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.customers.field.tier', 'en-US', 'Membership tier', 200, 'admin', 'admin/user/customers.html: 筛选标签（会员等级）', 1, now(), now()),
('admin.customers.field.tier', 'zh-CN', '会员等级', 200, 'admin', 'admin/user/customers.html: 筛选标签（会员等级）', 1, now(), now()),
('admin.customers.tier.none', 'en-US', 'Any tier', 200, 'admin', 'admin/user/customers.html: 等级选项（不限）', 1, now(), now()),
('admin.customers.tier.none', 'zh-CN', '不限等级', 200, 'admin', 'admin/user/customers.html: 等级选项（不限）', 1, now(), now()),
('admin.customers.tier.note', 'en-US', 'Tiers are assigned by the membership module from spend thresholds (or set manually in the back office); combined with the conditions above, all of them must hold.', 200, 'admin', 'admin/user/customers.html: 等级筛选说明', 1, now(), now()),
('admin.customers.tier.note', 'zh-CN', '等级由会员模块按消费门槛自动评定（或后台手工指定）；与上面几个条件同时选时全部都要成立。', 200, 'admin', 'admin/user/customers.html: 等级筛选说明', 1, now(), now());
