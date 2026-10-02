-- 499 · 系统设置页的 hint 改准（接了 SEO 之后）。
--
-- 为什么必须改：497 的原文写「默认国家与默认货币目前只是全局默认 + 兜底，还没有读取方」——
--   本批把币种接到了**展示 / 结构化数据层**（SEO 的 priceCurrency），hint 不改就是页面骗人。
--   新口径要说清三件事：谁在用（SEO 结构化数据与展示）、谁没用（订单仍固定 CNY）、
--   为什么不一起接（多币种是独立特性：要汇率、要变体定价、要展示换算）。
--
-- 覆盖而不是新插：这两条 key 在 497 已 seed，seed 一律 ON CONFLICT DO NOTHING ——
--   只改模板 fallback 会被库里的旧值盖住（317 / 493 记过同一坑）。
--
-- 幂等：WHERE item_value <> 新值，注册侧 ConditionSQL 判两语言都是新值则跳过。
WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.system.hint.trade', 'zh-CN', '默认国家与默认货币是全局默认 + 兜底（工程级覆盖走站点设置页）。币种目前用于 SEO 结构化数据与页面展示；订单与购物车的金额仍固定按人民币计算，多币种（汇率 / 变体定价 / 换算展示）是后续独立特性。'),
  ('admin.system.hint.trade', 'en-US', 'Default country and currency are global defaults + fallback (project-level overrides live on the site settings page). The currency is used for SEO structured data and display; orders and carts still compute amounts in CNY only — multi-currency (exchange rates, per-variant pricing, converted display) is a separate later feature.'),
  ('admin.system.help.trade', 'zh-CN', '默认国家与默认货币是全局默认 + 兜底。币种已用于 SEO 结构化数据（priceCurrency）与展示；订单仍固定 CNY，多币种是后续特性。'),
  ('admin.system.help.trade', 'en-US', 'Default country and currency are global defaults + fallback. The currency now feeds SEO structured data (priceCurrency) and display; orders remain CNY-only and multi-currency is a later feature.')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key AND s.lang = v.lang AND s.item_value <> v.item_value;
