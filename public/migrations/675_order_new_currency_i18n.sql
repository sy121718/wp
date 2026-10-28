-- 675 · 后台代客建单页的币种下拉（迁移 633 之后，后台可以建外币单）。
--
-- 背景：C 缺陷修好之后，CreateOrderReq 已经能带 currency，真实链路也会按当时的汇率
-- 写一份快照 —— 但后台建单页没有币种字段，页面上根本建不出外币单，那条能力等于不存在。
--
-- 三个界面 key 一组：下拉标签、默认项文案、页内提示。默认项不叫「不填写」：
-- 币种有确定的默认值（站点默认币），空值表达的是「取默认」，不是「这一项没有值」。
-- 外加一个错误 key（order.err.currencyUnknown）：币种白名单校验的拒绝文案，
-- 后台建单页会把它直接摊在顶部提示栏里，所以与这三个界面 key 同一批 seed。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.order_new.field.currency', 'zh-CN', '订单币种', 200, 'order', '代客建单页：订单币种下拉', 1, now(), now()),
('admin.order_new.field.currency', 'en-US', 'Order currency', 200, 'order', 'Order create form: currency select', 1, now(), now()),
('admin.order_new.field.currency_default', 'zh-CN', '用站点默认币', 200, 'order', '代客建单页：币种下拉的默认项', 1, now(), now()),
('admin.order_new.field.currency_default', 'en-US', 'Site default currency', 200, 'order', 'Order create form: default currency option', 1, now(), now()),
('admin.order_new.hint.currency', 'zh-CN', '外币单按当前汇率写一份快照；没有配汇率也能下单，报表里显示「未固化」。', 200, 'order', '代客建单页：币种说明', 1, now(), now()),
('admin.order_new.hint.currency', 'en-US', 'Foreign-currency orders snapshot the current rate. You can still order without a configured rate; reports show it as unconverted.', 200, 'order', 'Order create form: currency hint', 1, now(), now()),
('order.err.currencyUnknown', 'zh-CN', '该币种不在系统货币字典里（可能已被停用），请重新选择', 409, 'order', '建单币种白名单校验', 1, now(), now()),
('order.err.currencyUnknown', 'en-US', 'That currency is not in the site currency dictionary (it may be disabled); pick another one', 409, 'order', 'Order currency whitelist check', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
