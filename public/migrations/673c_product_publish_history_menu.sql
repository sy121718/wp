-- 673c · 商品发布记录的「发布历史」按钮（迁移 673 / B11）。
--
--   为什么是按钮菜单（type=3）而不是页面：发布历史不是导航目标 —— 它是商品行上的一个只读
--   详情，用户从「发布记录」页的某一行点开，不存在「直接打开发布历史页」的入口。
--   按钮菜单的作用是**授权**：sys_menus 里那行 type=3 记录的 permission_code 决定谁看得见
--   这个按钮（adminui.Prepare 注入 Buttons，模板用 isset(.Buttons[...]) 判定）。
--
--   权限码用 product:publish_attempts：读的是「谁在什么时候点发布、失败原因原文」（运维审计
--   信息），复用 product:get / list 会让只该看商品的人看到全站失败堆栈。
--
--   幂等：NOT EXISTS(title_key) 守卫；父菜单按 path 找（与 644 的写法一致）。

INSERT INTO sys_menus (title, title_key, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '发布历史',
       'product.publish_attempts',
       COALESCE((SELECT id FROM sys_menus WHERE path = '/admin/products/publish-records' AND type = 2 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1), 0),
       3, '', '', 'product:publish_attempts', 1, 80, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title_key = 'product.publish_attempts' AND type = 3 AND deleted_at IS NULL);
