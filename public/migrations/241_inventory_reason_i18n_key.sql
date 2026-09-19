-- 241 · 变动原因字典的 name 收口为 i18n key（库存域收口）。
--
-- 产品决策：变动原因的 code 是枚举，名称是**文案**；文案的真源只能是 sys_i18n
--（内容 → 文案词条，全站统一），库存域只管「有哪些原因、方向、启停」。
-- 原因表自己存文案会出现两份真相：改了 sys_i18n 不生效、改了原因表又绕过后台词条管理。
--
-- 迁移后：
--   · 内置原因（project_id IS NULL）的 name = inventory.reason.<code>，词条由 242 seed；
--   · 自定义原因的 name = inventory.reason.custom.<project_id>.<code>，
--     词条在「保存原因」时由 service 写入（这里把存量的一并搬过去：原文案写进 sys_i18n 的
--     zh-CN 一行，原因行改成指向该 key）。
--   key 里带工程 id 的原因：sys_i18n 是全局表，两个工程各建一个同 code 的自定义原因时，
--   不带工程 id 的 key 会互相覆盖（后建工程的文案悄悄把前一个改掉）。
--
-- 幂等：只处理「还没 key 化」的行（name NOT LIKE 'inventory.reason.%'）；
-- 重跑时条件不再成立，不会把已经写好的词条覆盖回旧文案。

-- 1) 内置原因：按 code 派生 key（全工程共用一条词条）。
UPDATE inventory_change_reasons
   SET name = 'inventory.reason.' || lower(code), update_time = now()
 WHERE project_id IS NULL
   AND name IS NOT NULL
   AND name NOT LIKE 'inventory.reason.%';

-- 2) 存量自定义原因：先把原文案写进 sys_i18n，再把行改成指向 key。
--    ON CONFLICT DO NOTHING：词条是「后台可改」的，重跑不得覆盖运营已经改过的文案。
WITH moved AS (
    SELECT project_id,
           'inventory.reason.custom.' || lower(project_id::text) || '.' || lower(code) AS item_key,
           name AS item_value
    FROM inventory_change_reasons
    WHERE project_id IS NOT NULL
      AND name IS NOT NULL
      AND name <> ''
      AND name NOT LIKE 'inventory.reason.%'
)
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT m.item_key, 'zh-CN', m.item_value, 200, 'inventory',
       'public/migrations/241_inventory_reason_i18n_key.sql', 1, now(), now()
  FROM moved m
ON CONFLICT (item_key, lang) DO NOTHING;

UPDATE inventory_change_reasons
   SET name = 'inventory.reason.custom.' || lower(project_id::text) || '.' || lower(code),
       update_time = now()
 WHERE project_id IS NOT NULL
   AND name IS NOT NULL
   AND name <> ''
   AND name NOT LIKE 'inventory.reason.%';