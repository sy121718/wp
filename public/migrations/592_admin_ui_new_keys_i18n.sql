-- 592 · 后台新增提示文案的词条（4 个 key，中英成对）。
--
-- 背景：页面鉴权与菜单码收口这一批引入了 4 句页面文案 —— 数据规则编辑页的「返回编辑」
-- 入口、（文案）词条删除提示、商品页写动作成功回执、商品批量动作未勾选提示。
-- i18n 门禁（scripts/check-i18n-keys-seeded.sh）只认 **sys_i18n 里的行**，
-- 模板与 Go 里写了 key 而没有 seed 就判红 —— 所以这里补上真源，而不是只留兜底文案。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register 里**逐条枚举**
-- 本批的 4 个 item_key（上界封闭）——前缀 LIKE 会被同期其它批次的行满足而静默跳过，
-- 全库总量则会让本批每次启动重跑（本仓库两种都踩过，理由见 226/277/283）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.datarules.back_to_edit', 'zh-CN', '返回编辑', 200, 'admin', '数据规则编辑页的返回入口（admin_page.go）', 1, now(), now()),
('admin.datarules.back_to_edit', 'en-US', 'Back to editing', 200, 'admin', '数据规则编辑页的返回入口（admin_page.go）', 1, now(), now()),
('admin.i18n.deleted', 'zh-CN', '词条已删除', 200, 'admin', '文案词条删除后的回执（admin_jump.go）', 1, now(), now()),
('admin.i18n.deleted', 'en-US', 'Entry deleted.', 200, 'admin', '文案词条删除后的回执（admin_jump.go）', 1, now(), now()),
('admin.products.actionDone', 'zh-CN', '操作已完成', 200, 'admin', '商品页写动作成功回执（product_jump.go）', 1, now(), now()),
('admin.products.actionDone', 'en-US', 'Done.', 200, 'admin', '商品页写动作成功回执（product_jump.go）', 1, now(), now()),
('admin.products.bulk.noneSelected', 'zh-CN', '未选择任何商品', 200, 'admin', '商品批量动作未勾选任何行时的提示（product_jump.go）', 1, now(), now()),
('admin.products.bulk.noneSelected', 'en-US', 'No products selected.', 200, 'admin', '商品批量动作未勾选任何行时的提示（product_jump.go）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
