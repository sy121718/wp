-- 591 · 导航菜单页写动作成功回执的词条（admin.navigations.actionDone）。
--
-- 背景：写动作的结论不再经 303 + ?err= / ?done= 回带列表页，改由 shell.RenderJump 渲染
-- 整页提示（对应 ThinkPHP 的 success() / error()）。成功提示需要一句「操作已完成」，
-- 而本模块此前没有任何写动作成功文案 —— 这句是本批新增的。
--
-- 失败文案不需要新 key：navigationErrPageText 走的是 NavigationFacingMessages 白名单
-- （ErrInvalidParam / ErrPathTaken / ErrStaleVersion …，迁移 058 / 290 已 seed），
-- 未命中回落 navigation.err.internal（迁移 268）。
--
-- 占位符：无。幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据是 register 里
-- 逐条枚举的 item_key 集合（上界封闭），不用 LIKE 前缀、也不用全库总量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.navigations.actionDone', 'zh-CN', '操作已完成', 200, 'admin', '导航菜单页写动作成功回执（提示页）', 1, now(), now()),
('admin.navigations.actionDone', 'en-US', 'Done.', 200, 'admin', '导航菜单页写动作成功回执（提示页）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
