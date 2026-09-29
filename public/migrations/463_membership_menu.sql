-- 463 · 会员等级与权益的后台菜单（sys_menus）。
--
-- 与 140/143/149/150/153 同构：菜单（type=2）挂在「交易」目录（id=120 那条 type=1 的目录，
-- 由 030 系列 seed）下，绑定查看权限点（membership:tier_list / membership:assign_list）。
-- 未 seed 时页面本身仍可直接访问 /admin/membership。
--
-- 为什么挂「交易」而不是新开一个目录：会员等级的输入是**订单消费额**、输出是**订单折扣与运费**，
-- 与订单管理 / 优惠码 / 客户管理是同一件事的不同侧面。另开顶层目录会让「订单 → 客户 → 会员」
-- 这条链路被拆到两处。
--
-- 侧栏数据源就是 sys_menus（shell.PermContextMiddleware → admin.BuildAuthorizedTree →
-- shell.BuildNav），因此本迁移**会**真的改变侧栏，而不只是「菜单管理」页的可选清单。
-- 权限过滤照常生效：没有 membership:tier_list 的账号看不到这一项。
--
-- title_key 是 sys_menus 的 i18n 标题列（迁移 057）：命中词条时侧栏按当前语言显示译文，
-- 未命中回退 title 原文。两个 key 的词条在 462a 一并 seed —— 菜单与它的译文同批到达，
-- 不会出现「侧栏显示裸 key」的中间态。
--
-- 幂等：WHERE NOT EXISTS 按 path + type 判重（不用 title：标题可能重名，而 path 是这条菜单的
-- 实际落点，重放时它才是判据）。重复执行安全。
--
-- 注意（AGENTS.md §数据库）：**权限点不写在本迁移里**。permission.SyncToDB 在装配末尾把
-- codes.go 的声明幂等 upsert 进 sys_permission 并补超管策略；152/153 里的权限点 INSERT 是
-- 旧形态，不要照抄（本文件只写 sys_menus 行，permission_code 引用的是已在 codes.go 登记的常量）。

INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '会员等级与权益', 'admin.membership.menu.tiers',
       COALESCE((SELECT id FROM sys_menus WHERE title = '交易' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/admin/membership', 'view.membership', 'membership:tier_list', 1, 5, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/membership' AND type = 2 AND deleted_at IS NULL);

INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '会员归属', 'admin.membership.menu.assignments',
       COALESCE((SELECT id FROM sys_menus WHERE title = '交易' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/admin/membership/assignments', 'view.membership_assignments', 'membership:assign_list', 1, 6, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/membership/assignments' AND type = 2 AND deleted_at IS NULL);
