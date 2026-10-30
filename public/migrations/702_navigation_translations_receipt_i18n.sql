-- 导航译文保存回执的两条整句词条（admin.navigation_translations.saved / .savedNone）。
--
-- 背景：译文保存的成功回执此前直接用 enums.Saved 当响应 message，取词后把 `%s` 原样漏给
-- 运营（"Saved %s translation(s)"）。修复一侧是出口（pkg/response 的 SuccessMsg 走 ArgsText
-- 填 %s 位置参数），另一侧就是这里：**占位符必须是 %s**，不能用 %d ——
-- ArgsText 只在「模板全是 %s / %[n]s / %%」时才注入，含 %d 就整句降级成 key 原文。
-- 451 里同一 key 的 zh-CN 原文是 %d，与本批的 %s 载荷不兼容。
--
-- 为什么需要一条新迁移而不是改 451：迁移是历史快照（public/CLAUDE.md），改历史 SQL 会让
-- 已跑过 451 的库里两种形状并存，且重置后行为与线上不一致。
-- 这里用 DO UPDATE 覆盖（与 697 的 DO NOTHING 不同）：本批的目的正是**替换**那两条原文，
-- 保留旧值等于保留了「%d 漏给运营」的那句。中文兜底一并覆盖，保证中英两侧占位符一致。
--
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE —— seed 是默认值来源，后台仍是真相来源；
-- 运营改过的词条在这里会被本批覆盖一次（本批的定义就是「这两个 key 的值应当是这两句」）。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.navigation_translations.saved', 'zh-CN', '已保存 %s 条译文；已标记受影响页面待重建（下次构建生效）。'),
('admin.navigation_translations.saved', 'en-US', 'Saved %s translation(s); affected pages are marked for rebuild (effective on the next build).'),
('admin.navigation_translations.savedNone', 'zh-CN', '没有需要写入的变化。'),
('admin.navigation_translations.savedNone', 'en-US', 'There were no changes to write.')
ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value, update_time = now();
