-- 428 · 翻译工作台合并为一张编辑表后新增的「组件」列头（docs/02-L P1-5）
--
-- 背景：admin/page_translations.html 此前是「每个组件一张卡 + 一张表」，八段表的 thead
-- 完全相同（字段 / 原文 / 译文 / 状态 / 来源），所有组又共用页头一个「保存全部」。
-- 合并成一张表后，组件名从每组一个 <h2> 变成表内合并整行的分组行，表头多出第一列「组件」。
--
-- 为什么必须有一条迁移而不是只改模板：模板里的中文只是 t(key, 兜底) 的 fallback，
-- 词条命中时显示的是库里的值（与 192 / 317 / 413 / 415 同一手法）。
-- 只改模板 = 英文界面下这一列回落成中文「组件」，而同排的其它五列都是英文。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本票**只新增、不修改任何既有词条**。
--   ConditionSQL 由迁移器 db.Raw 直接执行，没有参数替换，判定用的 key 只能写进 SQL 字面量。
--   新 key 挑 en-US 行做门槛：zh-CN 行与模板 fallback 同形，容易被别处顺手加上，
--   按 zh-CN 计数会让门槛在「本批还没跑」时就成立、整批词条被静默跳过。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.page_translations.col.component', 'zh-CN', '组件', 200, 'admin', 'admin/page_translations.html: 合并为一张编辑表后的「组件」列头（原每组 h2 的分组名）', 1, now(), now()),
('admin.page_translations.col.component', 'en-US', 'Component', 200, 'admin', 'admin/page_translations.html: 合并为一张编辑表后的「组件」列头（原每组 h2 的分组名）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
