-- 602 · 导航同级排序并列的业务文案（1 个 key，中英成对）。
--
-- 背景（task-84）：页面「上移/下移」从「handler 连调两次 Update」改成一次原子交换
-- （service.SwapMove → model.SwapSortOrders，同一事务两条条件 UPDATE）。
-- sort_order 上没有唯一约束（「同级唯一」只是应用层不变量：Create 用 MaxSortOrder+1 追加），
-- 外部脚本按 /api/navigation/update 裸置位或手工写库都能造出同级并列；此时交换两行的取值
-- 等于什么都没换（并列由 id ASC 决胜）。修复前那种写法会连着两次写入、最后一次「看着成功」，
-- 现在是显式零写 + 一句能说清后果的业务文案。
--
-- 为什么不复用「已完成」（admin.navigations.actionDone，迁移 290 时代的通用句）：
-- 并列时**没有发生位移**，用成功口径回话会把没做的事说成做了（review-sol 在 task-84 预审里
-- 明确要求 tied 走失败样式）。也不复用 ErrStaleVersion：那句说的是「别人改过、你刷新即可」，
-- 而并列是**库内状态**问题，刷新不解决。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— 不覆盖运营在后台改过的词条；
-- 门槛判据在 register 里**逐条枚举**本批 1 个 item_key × 两种语言（上界封闭）：
-- 只数中文那行会在「英文缺行」时静默跳过（本仓库踩过，见 226/277/283/592/598/600/601）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrSortTied', 'zh-CN', '同一级里存在重复的排序值，本次没有产生位移；请先把这一级的顺序理顺', 409, 'error', 'internal/module/navigation/enums/navigation_enums.go: 同级排序并列（SwapMove 零写，走失败样式）', 1, now(), now()),
('ErrSortTied', 'en-US', 'This level has duplicate sort values, so nothing moved; tidy the order at this level first', 409, 'error', 'internal/module/navigation/enums/navigation_enums.go: tied sort values (SwapMove writes nothing, failure-style prompt)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
