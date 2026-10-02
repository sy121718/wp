-- 504 · 后台代客建单页新增「国家 / 地区」下拉的两个文案位（中英成对，4 行）。
--
-- 为什么必须同批 seed：模板里的中文只是 t() 兜底，**词条命中时显示的是库里的值** ——
--   只加模板不 seed，英文界面照样显示中文（缺 en-US 时不会有任何断言变红，
--   与 433 记的是同一个坑）。
--
-- 与 433 的关系：433 seed 了同一页其余字段标签，本批只**新增**两个 key，不动 433 的任何行
--（历史迁移保持原样；seed 的幂等由 ON CONFLICT 保证）。
--
-- 这两个 key 的用途：
--   · admin.order_new.field.country      —— 收货 / 账单两处下拉的字段标签（同一份文案）；
--   · admin.order_new.field.country_none —— 下拉首位「（不填写）」空项：国家是**选填**，
--     非中国站点或「这单不需要寄」时运营要能明确地留空，而不是被默认值逼着照错填。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（唯一约束 uk_sys_i18n_key_lang）。
--   注册侧 ConditionSQL 按**本批自己的两个 key** 计数（逐条枚举，不用 LIKE 前缀、
--   不用全库总量）：用全库总量会被别的批次的行满足而静默跳过（058 踩过）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.order_new.field.country', 'zh-CN', '国家 / 地区', 200, 'admin', 'admin/order/order_new.html: 国家/地区字段标签（收货与账单共用）', 1, now(), now()),
    ('admin.order_new.field.country', 'en-US', 'Country / Region', 200, 'admin', 'admin/order/order_new.html: 国家/地区字段标签（收货与账单共用）', 1, now(), now()),
    ('admin.order_new.field.country_none', 'zh-CN', '（不填写）', 200, 'admin', 'admin/order/order_new.html: 国家下拉的「（不填写）」空项', 1, now(), now()),
    ('admin.order_new.field.country_none', 'en-US', '(not set)', 200, 'admin', 'admin/order/order_new.html: 国家下拉的「（不填写）」空项', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
