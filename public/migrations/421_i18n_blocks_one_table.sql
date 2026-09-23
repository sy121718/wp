-- 421 · 全局块管理页「一张表 + 影响面降级」的新增词条（审计 02-L P1-4 / P1-10）
--
-- 背景：internal/templates/admin/blocks.html 本批做两件事——
--   · P1-4 三段表（页眉 / 页脚 / 区块）合并成**一张表**，列的差异（5 / 5 / 7）与
--     全选框的唯一性（靠三层嵌套 if 保证）一起消失，「是哪种块」改由「类型」列承担；
--   · P1-10「待重建影响面」从列表卡之上的**常驻只读卡**降级为页头一行徽章 + 折叠清单。
--
-- 三处新增文案（其余全部复用已有的 admin.blocks.*）：
--
--	admin.blocks.impact.badgeTail  —— 页头徽章 / 折叠区 summary 的计数后缀。
--	  原句是 admin.blocks.impact.lead（"当前有 "）+ N + impact.tail（" 个页面处于「有更新未发布」：…"），
--	  后半段带完整解释，塞不进一枚徽章；解释挪进 help 悬浮层，计数单独成一条。
--	admin.blocks.impact.help       —— 上述解释的正文（悬浮在徽章旁的 ? 上）。
--	admin.blocks.list_empty        —— 合并后**唯一**的空态。
--	  合并前每段各有一条空态（headers_empty / footers_empty / blocks_empty），
--	  合并成一张表后「哪一段空了」不再是界面上的分区概念（类型列与筛选承担这件事），
--	  空态只有一档 = 三类都没有；三条旧词条因此不再被任何模板取用。
--
-- 为什么三条旧词条**只留不删**：192 的 seed 由 register_admin_i18n.go 注册，而它的幂等
--   条件是**逐条枚举**本批这些 key 并计数的。删掉词条会让那条 seed 的判定不再成立、
--   每次启动把它们重新插回来（AGENTS.md「在 register 里做的删除，永远赢不过在 registerSeed
--   里重建它的 seed」）。真正的退役要连同 192 的 SQL 与那个幂等条件一起收口，
--   属专门的孤儿词条清理批次（先例：419 + cmd/i18n-orphan），不在本批范围内。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不修改任何既有词条，故无 UPDATE。
--   判定用的值只能写进 SQL 字面量：ConditionSQL 由迁移器 db.Raw 直接执行，没有参数替换。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.blocks.impact.badgeTail', 'zh-CN', ' 个页面有更新未发布', 200, 'admin', 'admin/blocks.html: 页头待重建徽章的计数后缀', 1, now(), now()),
('admin.blocks.impact.badgeTail', 'en-US', ' pages have unpublished changes', 200, 'admin', 'admin/blocks.html: 页头待重建徽章的计数后缀', 1, now(), now()),
('admin.blocks.impact.help', 'zh-CN', '块、内容、主题或导航改动过，产物的字节还停在旧版本。重新构建这些页面后新内容才会出现在访问面。', 200, 'admin', 'admin/blocks.html: 待重建徽章旁的悬浮解释', 1, now(), now()),
('admin.blocks.impact.help', 'en-US', 'Blocks, content, themes or navigation changed, but the published bytes still carry the old version. Rebuild these pages to publish the new content.', 200, 'admin', 'admin/blocks.html: 待重建徽章旁的悬浮解释', 1, now(), now()),
('admin.blocks.list_empty', 'zh-CN', '还没有全局块。页眉 / 页脚块在「主题管理 → 设置」里绑定到主题，构建页面时编译期内联；区块用于跨页面复用的结构片段，工作台「全局块」页签可一键引用。', 200, 'admin', 'admin/blocks.html: 合并成一张表后的唯一空态', 1, now(), now()),
('admin.blocks.list_empty', 'en-US', 'No global blocks yet. Bind header / footer blocks to a theme under Theme management → Settings so they are inlined into pages at build time; blocks are structure fragments reused across pages and can be inserted from the Global blocks tab in the workbench.', 200, 'admin', 'admin/blocks.html: 合并成一张表后的唯一空态', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
