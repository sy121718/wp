-- 491 · pages.excluded_langs：页面级语言排除（本页不产出、不参与切换器/hreflang/sitemap 的语言）。
--
-- 语义（同时写进列的 COMMENT）：
--   排除的语言 = **本页不产出**、不参与切换器 / hreflang 互指 / sitemap 的语言；
--   空数组 = 全部站点语言都产出（默认行为，存量行因此零变更）。
--
-- 为什么是页面级的列而不是工程级设置：同一个工程里，某些页面只做中文（法务 / 客服），
-- 另一些做全语言 —— 这是**单页**的产出范围，不是站点语言清单的子集。
--
-- **本文件只允许幂等语句**（`ADD COLUMN IF NOT EXISTS` / `COMMENT ON`），理由见
-- register_page_excluded_langs.go：这条结构迁移刻意**不带 TableName**，于是每次启动都会
-- 整条执行（靠语句自身幂等），而不是靠 migrator 的「表存在即跳过」——
-- pages 表**早就存在**，带上 TableName 会让整条迁移被跳过、ALTER 永不执行，
-- 而全新库会执行，两边从此分叉。将来再改 pages 结构要另开一条新迁移。

ALTER TABLE pages
    ADD COLUMN IF NOT EXISTS excluded_langs text[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN pages.excluded_langs IS
    '本页排除的语言（完整语言码）：不产出、不参与语言切换器 / hreflang / sitemap；空数组 = 全部站点语言都产出';
