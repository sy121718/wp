-- 493 · 覆盖 `admin.settings.locales.hint.mode_intro`（站点设置页的语言方案说明：旧口径 → 新口径）
--
-- 为什么要一条**覆盖**迁移：这个词条讲的是「语言 URL 方案配在哪里」。它原先写的是
-- 「访问路径的语言方案由下列配置项控制：`i18n.site_lang_url_mode`」——而那个 config.yaml 键
-- **已经被删除**（默认值搬进 sys_config 的 i18n 组，且工程级覆盖按工程读）。留着旧文案，
-- 运营会去找一个不存在的 yaml 键，而真正该动的是本页那个开关。
--
-- 为什么不能只改模板 fallback：模板的取值链是 t(key, fallback)，**fallback 只在词条缺失时生效**，
-- 而这条词条早就 seed 过 —— 只改模板等于「三处都写对了、只有库里那一行说了算」。
-- 与 254 / 316 / 317 同一手法：用 UPDATE 覆盖旧值，新库与存量库结果一致；
-- 历史迁移保持原样（AGENTS.md：seed 可重复执行要同步改，历史迁移 SQL 不回改）。
--
-- 幂等：WHERE `item_value <> 新值`，注册侧 ConditionSQL 判「两语言都已是新文案则跳过」。
-- 覆盖：1 个 key × 2 语言。
WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.settings.locales.hint.mode_intro', 'zh-CN', '访问路径的语言方案由本页的「语言 URL 方案」开关控制（工程级，保存即生效）；未配置时跟随系统配置里的全局默认。'),
  ('admin.settings.locales.hint.mode_intro', 'en-US', 'The language URL scheme is controlled by the "Language URL scheme" switch on this page (per project, effective on save); when unset it follows the global default in the system config.')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key AND s.lang = v.lang AND s.item_value <> v.item_value;
