-- 254 · 富文本说明文案纠正（4 个 key × 中英）—— 迁移 188 的 seed 文案已过期
--
-- 背景：188 写这些文案时，清洗白名单还会把 h1 降级成 h2、且不含表格与折叠块（details/summary）。
-- 现在真实行为是：h1~h5 原样保留（降级早已取消）、表格 / 折叠块 / 水平线都在白名单内。
-- 188 用的是 INSERT ... ON CONFLICT DO NOTHING，改它的 SQL 对**存量库无效**（键已存在即跳过），
-- 历史迁移也不回改（AGENTS.md：seed 可重复执行要同步改，历史迁移 SQL 保持原样）；
-- 因此这里用一条 UPDATE 迁移把两个语言的旧值覆盖成新文案，新库与存量库结果一致。
--
-- 幂等：WHERE item_value <> 新值 且注册侧 ConditionSQL 判「已是新文案则跳过」，重复执行不动行。

WITH v(item_key, lang, item_value) AS (VALUES
  ('admin.article.edit.bodyHint', 'zh-CN',
   '正文支持标题（h1~h5 原样保留，不降级）、段落、列表、引用、代码块、表格、折叠块、水平线、链接与图片；白名单之外的元素（script / style / iframe 等）与一切 on* 事件属性会在保存时被清洗掉。正文里写的标题会进入 SEO 的标题结构评测。'),
  ('admin.article.edit.bodyHint', 'en-US',
   'The body supports headings (h1–h5 kept as-is, never downgraded), paragraphs, lists, quotes, code blocks, tables, collapsible blocks, horizontal rules, links and images. Elements outside the allowlist (script / style / iframe …) and every on* event attribute are stripped on save. Headings written in the body feed the SEO heading-structure check.'),
  ('admin.article.edit.importWhitelistMid', 'zh-CN',
   '正文保存时会过一遍富文本白名单（段落、标题 h1~h5、列表、引用、代码块、表格、折叠块、水平线、链接、图片），'),
  ('admin.article.edit.importWhitelistMid', 'en-US',
   'A rich-text allowlist (paragraphs, headings h1–h5, lists, quotes, code blocks, tables, collapsible blocks, horizontal rules, links, images) runs over the body when it is saved, '),
  ('admin.article.edit.importWhitelistStrong', 'zh-CN',
   '白名单之外的元素'),
  ('admin.article.edit.importWhitelistStrong', 'en-US',
   'elements outside the allowlist'),
  ('admin.article.edit.importWhitelistTail', 'zh-CN',
   ' —— 它们在保存那一刻就被剥掉了，导入自然也看不到。需要别的块级元素，请到画布里添加对应组件。'),
  ('admin.article.edit.importWhitelistTail', 'en-US',
   ' — they are stripped at that moment, so the import cannot see them either. If you need another block-level component, add it on the canvas.')
)
UPDATE sys_i18n s
   SET item_value = v.item_value, update_time = now()
  FROM v
 WHERE s.item_key = v.item_key
   AND s.lang = v.lang
   AND s.item_value <> v.item_value;
