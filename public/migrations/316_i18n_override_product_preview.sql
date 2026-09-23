-- 316 · i18n 词条覆盖（商品操作列「预览」：把 191 的「预览详情页」缩成两个字）
--
-- 背景：操作列按钮文案太长，用户明确要求缩成「预览」。模板的取值链是 t(key, fallback) ——
--       **fallback 只在词条缺失时生效**，而 admin.products.row.preview 早在迁移 191
--       就已 seed 成「预览详情页」。314 里再插一次同 key 时 ON CONFLICT DO NOTHING
--       静默跳过，于是页面上显示的仍是 191 的旧值：模板、fallback、314 三处都写着「预览」，
--       只有库里那一行说了算。这是「以为改了、其实没改」的典型形态。
--
-- 与 254（富文本说明文案纠正）同一手法：用 UPDATE 迁移覆盖旧值，新库与存量库结果一致；
-- 历史迁移 191 保持原样（AGENTS.md：seed 可重复执行要同步改，历史迁移 SQL 不回改）。
--
-- 幂等：WHERE item_value <> 新值，且注册侧 ConditionSQL 判「两语言都已是新文案则跳过」——
--       重复执行不动任何行。
-- 覆盖：1 个 key × 2 语言。
WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.products.row.preview', 'zh-CN', '预览'),
  ('admin.products.row.preview', 'en-US', 'Preview')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key
   AND s.lang = v.lang
   AND s.item_value <> v.item_value;
