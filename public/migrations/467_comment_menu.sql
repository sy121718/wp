-- 467 · 评论审核的后台菜单（sys_menus）。
--
-- 与 463（会员）/ 150 / 153 同构：菜单（type=2）挂在「内容」目录（id 由 title + type 反查，
-- 不写死 id）下，绑定查看权限点（comment:list）。未 seed 时页面本身仍可直接访问
-- /admin/comments。
--
-- 为什么挂「内容」而不是交易 / 商品：评论当前两种挂载（article / product）里，
-- 文章评论是主场景（博客站的评论量远大于商品评价），且「内容」目录本来就收文章与页面；
-- 商品评价将来若要做成独立视图，再在这一页上按实体类型筛选即可，不必另开目录。
--
-- 侧栏数据源就是 sys_menus（shell.PermContextMiddleware → admin.BuildAuthorizedTree →
-- shell.BuildNav），因此本迁移会**真的**改变侧栏，而不只是「菜单管理」页的可选清单。
-- 权限过滤照常生效：没有 comment:list 的账号看不到这一项。
--
-- title_key 是 sys_menus 的 i18n 标题列（迁移 057）：命中词条时侧栏按当前语言显示译文，
-- 未命中回退 title 原文。该 key 的词条在 466a 一并 seed —— 菜单与它的译文同批到达，
-- 不会出现「侧栏显示裸 key」的中间态。
--
-- 幂等：WHERE NOT EXISTS 按 path + type 判重（不用 title：标题可能重名，而 path 是这条菜单的
-- 实际落点，重放时它才是判据）。重复执行安全。
--
-- 注意（AGENTS.md §数据库）：**权限点不写在本迁移里**。permission.SyncToDB 在装配末尾把
-- codes.go 的声明幂等 upsert 进 sys_permission 并补超管策略；本文件只写 sys_menus 行，
-- permission_code 引用的是已在 codes.go 登记的常量（comment:list）。

INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '评论审核', 'admin.comment.menu',
       COALESCE((SELECT id FROM sys_menus WHERE title = '内容' AND type = 1 AND deleted_at IS NULL), 0),
       2, '/admin/comments', 'view.comments', 'comment:list', 1, 30, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE path = '/admin/comments' AND type = 2 AND deleted_at IS NULL);
