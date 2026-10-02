-- 503 · 系统设置页的 trade 提示补上「改币种不换算金额」这个前提。
--
-- 号段说明：原用 501，与并行的 501_order_ship_country.sql（Migrations 台账）撞号，
--   已让号到 503（详见 register_system_settings_hint_currency_no_conversion.go 的头注）。
--
-- 为什么必须补：500 只说了「影响新订单的币种标签与页面展示」，运营很容易读成
--   「改一下就能卖美元」。但本系统**不做汇率换算** —— 金额始终是数值（分），
--   币种只是这个数字的标签。改它的正确含义是「声明本站商品定价本来就是该币种」。
--   若商品价格按人民币填（19.90 元），把默认货币改成 USD 后前台显示的是
--   「$19.90」而不是换算后的价格。这句不写，运营改配置就是一次静默的定价事故。
--
-- 覆盖而不是新插：这两条 key 在 497 已 seed，seed 一律 ON CONFLICT DO NOTHING ——
--   只改模板 fallback 会被库里的旧值盖住（317 / 493 / 499 / 500 记过同一坑）。
--
-- 与 500 的条件串共存（重要）：新文案**保留**了 500 ConditionSQL 依赖的四个特征串
--   （没有货币选择器 / no currency picker / 改的是地区，不是货币 /
--   change their region, not the currency）。去掉其中任何一个，500 都会判定「不是新值」
--   而每次启动重跑，把本批的改动覆盖回旧文案 —— 076 记过这个方向的故障。
--
-- 幂等：WHERE item_value <> 新值，注册侧 ConditionSQL 判四条都是新值则跳过。
WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.system.hint.trade', 'zh-CN', '默认国家与默认货币是全局默认 + 兜底（工程级覆盖走站点设置页）。货币由后台全局限定为单值：它影响新订单的币种标签与页面展示（含 SEO 结构化数据）；前台用户只能改自己的地区（国家 / 省），没有货币选择器。已下单的订单保留当时的币种快照。注意：改币种只改标签，不换算金额 —— 它等于声明本站商品定价本来就是该币种；若商品价格按人民币填，改成美元后显示的是同样的数字（如 ¥19.90 显示为 $19.90），不是换算后的价格。'),
  ('admin.system.hint.trade', 'en-US', 'Default country and currency are global defaults + fallback (project-level overrides live on the site settings page). The currency is a single global value: it sets the currency label of new orders and of on-page display (including SEO structured data). Storefront users can only change their own region (country / province) — there is no currency picker. Orders already placed keep the currency snapshot taken at checkout. Note: changing the currency changes the label only and does not convert amounts — it declares that your product prices are already denominated in that currency. If prices were entered in CNY and you switch to USD, they display as the same number (e.g. CNY 19.90 shows as USD 19.90), not a converted amount.'),
  ('admin.system.help.trade', 'zh-CN', '货币由后台全局限定为单值，影响新订单与展示（含 SEO 结构化数据）；前台用户改的是地区，不是货币。改币种只改标签、不换算金额 —— 改它等于声明本站商品定价本来就是该币种。'),
  ('admin.system.help.trade', 'en-US', 'The currency is a single global value; it affects new orders and display (including SEO structured data). Storefront users change their region, not the currency. Changing it changes the label only and does not convert amounts — setting it declares that your product prices are already denominated in that currency.')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key AND s.lang = v.lang AND s.item_value <> v.item_value;
