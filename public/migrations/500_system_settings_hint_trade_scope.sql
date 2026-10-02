-- 500 · 系统设置页的 trade 提示按**产品口径**改准（币种是全局限定，不是「没有读取方」）。
--
-- 口径（产品澄清）：货币由后台全局限定为**单值**；前台不提供货币选择，用户只能改自己的
--   地区（国家 / 省）。币种是**标签 / 口径**而不是算术 —— 金额本来就是数值（分），
--   改币种只改标签；也不存在「同一商品按币种分别定价」。
--   因此本批把币种接到了展示层与**新订单**：SEO 结构化数据、购物车展示、下单快照。
--
-- 499 写的是「订单与购物车仍固定按人民币计算，多币种是后续特性」—— 那是上一轮我在
--   多币种语义未澄清时的判断，本批按澄清后的口径接上了两处，文案必须跟着改：
--   页面骗人比功能缺失更难查。
--
-- 覆盖而不是新插：这两条 key 在 497 已 seed，seed 一律 ON CONFLICT DO NOTHING ——
--   只改模板 fallback 会被库里的旧值盖住（317 / 493 / 499 记过同一坑）。
--
-- 幂等：WHERE item_value <> 新值，注册侧 ConditionSQL 判两语言都是新值则跳过。
WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.system.hint.trade', 'zh-CN', '默认国家与默认货币是全局默认 + 兜底（工程级覆盖走站点设置页）。货币由后台全局限定为单值：它影响新订单的币种标签与页面展示（含 SEO 结构化数据）；前台用户只能改自己的地区（国家 / 省），没有货币选择器。已下单的订单保留当时的币种快照。'),
  ('admin.system.hint.trade', 'en-US', 'Default country and currency are global defaults + fallback (project-level overrides live on the site settings page). The currency is a single global value: it sets the currency label of new orders and of on-page display (including SEO structured data). Storefront users can only change their own region (country / province) — there is no currency picker. Orders already placed keep the currency snapshot taken at checkout.'),
  ('admin.system.help.trade', 'zh-CN', '货币由后台全局限定为单值，影响新订单与展示（含 SEO 结构化数据）；前台用户改的是地区，不是货币。'),
  ('admin.system.help.trade', 'en-US', 'The currency is a single global value; it affects new orders and display (including SEO structured data). Storefront users change their region, not the currency.')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key AND s.lang = v.lang AND s.item_value <> v.item_value;
