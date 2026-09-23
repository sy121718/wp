-- ========================================
-- 429 — 四个「已进白名单、却从未登记词条」的 key（命中时页面直接显示裸 key）
--
-- 背景：各域白名单（XxxFacingMessages / pageFacingKeys）与 sys_i18n 词条是**两处独立登记**，
-- 漏了后一处不会有任何报错或门禁变红 —— 取词函数只能按 fallback 回落，而这几个位置的
-- fallback 恰好就是 key 本身，于是页面上直接出现 `ErrInvalidRange` 这样的英文裸串。
-- 判据是结构性的：白名单里出现了一个 key，就必须能在库里查到它的词条。
--
-- 本批新增 4 个 key × 2 语言 = 8 行：
--   ErrInvalidRange   —— analytics 白名单（analyticsFacingMessages）；
--                        页面出口 internal/module/analytics/inbound/http/analytics_page_handle.go
--                        的 analyticsFacingError 命中后经 shell.TranslateFor 取词。
--   ErrInvalidParent  —— navigation 白名单（NavigationFacingMessages）；
--                        页面出口 navigation/inbound/http/navigation_err.go 的 navigationErrPageText
--                        → localizeFacing → response.TranslateMessage。
--   ErrInvalidSlot    —— page 白名单（pageFacingKeys）；
--                        页面出口 page/inbound/http/page_err.go 的 pageFacingKey / pageNoticeTexts
--                        都按 tr(key, key) 取词（fallback 就是 key 本身）。
--   ErrSlotPageMiss   —— 同上（要绑定的页面不存在 / 不属于当前工程）。
--
-- 为什么这几条值得单独一批：它们不是「少了一句提示」，而是把**内部常量名**摆到了运营面前。
-- 运营看到 `ErrInvalidSlot` 只能来报「页面上有串英文」，而那句话本可以告诉他「回列表页重选」。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**不修改任何既有词条的值**。
-- 判定（register 侧 ConditionSQL）由迁移器 db.Raw 直接执行、**没有任何参数替换** ——
-- 判定要用的 key 只能写成 SQL 字面量（写成 ? 会被换成表名、判定恒为 0 且每次启动重跑，178 踩过）。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrInvalidRange', 'zh-CN', '时间范围不合法：开始日期不能晚于结束日期，且跨度不超过 366 天', 400, 'analytics', 'internal/module/analytics/enums/analytics_enums.go: 白名单已登记但无词条，命中时页面曾显示裸 key', 1, now(), now()),
('ErrInvalidRange', 'en-US', 'Invalid date range: the start date cannot be later than the end date, and the span cannot exceed 366 days', 400, 'analytics', 'internal/module/analytics/enums/analytics_enums.go: whitelisted but had no i18n entry; the page used to show the bare key', 1, now(), now()),
('ErrInvalidParent', 'zh-CN', '父级菜单不合法：父项必须存在、与本项属于同一工程与同一类型，且不能形成循环', 400, 'error', 'internal/module/navigation/enums/navigation_enums.go: 进白名单 NavigationFacingMessages 但无词条', 1, now(), now()),
('ErrInvalidParent', 'en-US', 'Invalid parent item: it must exist, belong to the same project and the same type, and must not create a cycle', 400, 'error', 'internal/module/navigation/enums/navigation_enums.go: whitelisted (NavigationFacingMessages) but had no i18n entry', 1, now(), now()),
('ErrInvalidSlot', 'zh-CN', '槽位键不在允许范围内，请回到列表页重新选择', 400, 'error', 'internal/module/page/enums/page_enums.go: 进白名单 pageFacingKeys 但无词条（取词 fallback 就是 key 本身）', 1, now(), now()),
('ErrInvalidSlot', 'en-US', 'The slot key is not in the allowed set; go back to the list and choose another one', 400, 'error', 'internal/module/page/enums/page_enums.go: whitelisted (pageFacingKeys) but had no i18n entry', 1, now(), now()),
('ErrSlotPageMiss', 'zh-CN', '要绑定的页面不存在、已被删除，或不属于当前工程', 404, 'error', 'internal/module/page/enums/page_enums.go: 进白名单 pageFacingKeys 但无词条', 1, now(), now()),
('ErrSlotPageMiss', 'en-US', 'The page to bind does not exist, has been deleted, or does not belong to the current project', 404, 'error', 'internal/module/page/enums/page_enums.go: whitelisted (pageFacingKeys) but had no i18n entry', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
