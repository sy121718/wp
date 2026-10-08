-- ========================================
-- 401 — 侧栏「邮件活动」菜单项下线（死链收口）
--
-- 背景：sys_menus 里的「邮件活动」指向 /admin/mail/campaign —— 那是**单条活动的报表页**，
-- 以 ?id= 为必需参数（mail_marketing_page_handle.go 的 mailQueryID 前置判定），
-- 而菜单项不带任何参数。运营从侧栏点进来必然落到「请先从活动列表选择一条活动，
-- 再查看它的报表」的引导重定向 —— 一个点不通的菜单项。
--
-- 为什么是「隐藏」而不是「删除行」（对齐 224 第 7 段的既有范式）：
--   角色授权按 menu_id 收集 permission_code（GetPermissionCodesByIDs），
--   删行会让已授权角色**静默缩权**。隐藏的行仍参与授权收集，只是不进侧栏
--   （internal/shell/nav.go 丢掉 is_hidden=1 的条目）。
--
-- 为什么下线它不会少掉任何可授权项：本项的 permission_code 与「邮件营销」
--   （/admin/mail/marketing）**完全相同**，两条菜单登记的是同一个权限码。
--   保留其中一条即可，权限分配树里照样有这一项。
--
-- 报表入口没有消失：/admin/mail/marketing 的活动列表每行都有
--   「活动名 → /admin/mail/campaign?id=N」与「报表」按钮（mail_marketing.html:187/197）。
--
-- 为什么不改 path 把它指向营销页：那会让侧栏出现两个同名同目标的菜单项，比死链更糟。
--
-- 幂等：WHERE 带 is_hidden = 0，重复执行不改变结果；CheckSQL 表达「已完成」——
--   目标行为 0 条即跳过（与 225 同一形状）。
-- 回滚：UPDATE sys_menus SET is_hidden = 0 WHERE type = 2 AND path = '/admin/mail/campaign';
-- ========================================

UPDATE sys_menus
SET is_hidden = 1, update_time = NOW()
WHERE type = 2
  AND path = '/admin/mail/campaign'
  AND is_hidden = 0
  AND deleted_at IS NULL;
