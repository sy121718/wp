-- ========================================
-- 224 — 后台导航菜单收口：侧栏改由 sys_menus 驱动（唯一真源）
--
-- 背景：后台侧栏此前是 dashboard 包里的一张 Go 硬编码菜单表
-- （internal/module/dashboard/inbound/http/nav_menu.go 的 navConfig），
-- 与 sys_menus 双份维护且已彻底漂移 —— 一边是 /admin/orders、一边是 /orders
-- （Vue SPA 时代的浏览器路由），22 条与 27 条的交集只剩 1 条（/admin/plugins）；
-- 侧栏还漏掉了商品 / 库存 / 货源 / 采购 / 邮件 / 文案词条 / 变更记录 / 商品详情模板
-- 等十几个**页面确实存在**的入口，那些页面此前只能靠手输 URL 到达。
--
-- 现在 navConfig 已删除：admin 模块的 BuildAuthorizedTree 按 permission_code
-- 过滤出授权树（与 Casbin API 鉴权同源，超管拿全量策略），dashboard 只负责补
-- 当前页高亮与展开态。本迁移把表的菜单树修正成可渲染形态。
--
-- 做四件事：
--   1. 建 6 个业务分组目录（type=1），替代原来唯一的「站点工程」；
--   2. 现有菜单（type=2）路径改成实际页面路径（/admin 前缀），并归位到所属分组；
--   3. 补上侧栏漏掉、但页面确实存在的菜单；补「插件管理」缺失的查看权限码
--      （NULL 权限码谁都匹配不上 = 谁都看不见，切表后它会直接消失）；
--   4. Vue 时代遗留、今天没有对应页面的菜单标 is_hidden = 1 —— **保留行不删**：
--      角色授权按 menu_id 收集 permission_code（GetPermissionCodesByIDs），
--      删行会让已授权角色静默缩权。
--
-- 为什么不改既有菜单的 title：多条 seed 的 ConditionSQL 按 title 判定「是否已完成」
-- （如 084 的 title = '商品管理'、register_order.go 的 title IN ('订单管理','优惠码')），
-- 改名会让那些 seed 被判定为未执行而重跑。目录「站点工程」是唯一例外 —— 它从模块名
-- 变成分组名，而引用它的旧 seed 全部排在本迁移之前（新库按版本顺序执行同样如此）。
--
-- 幂等：UPDATE 写定值、INSERT 用 NOT EXISTS 守卫，重复执行结果一致。
-- ========================================

-- ---- 1. 目录：原「站点工程」改造为「内容」分组，另建 5 个业务分组 ----

UPDATE sys_menus
SET title = '内容', path = '', icon = 'file-text', sort_order = 2, update_time = NOW()
WHERE type = 1 AND title = '站点工程' AND deleted_at IS NULL;

INSERT INTO sys_menus (title, parent_id, type, path, icon, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title, 0, 1, '', v.icon, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('管理',       'shield-check',  1),
    ('商品与库存', 'boxes',         3),
    ('交易',       'shopping-cart', 4),
    ('站点',       'globe',         5),
    ('系统',       'settings',      6)
) AS v(title, icon, sort)
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus m WHERE m.title = v.title AND m.type = 1 AND m.deleted_at IS NULL
);

-- ---- 2. 现有菜单：路径改成实际页面路径 ----
--
-- 按 permission_code 匹配（现有菜单的 code 互不重复，是稳定键）。
-- 「插件管理」permission_code 为 NULL、「重定向管理」路径本就正确，两条不在下表內。

UPDATE sys_menus m
SET path = v.path, sort_order = v.sort, update_time = NOW()
FROM (VALUES
    ('page:list',                    '/admin/pages',               2),
    ('block:list',                   '/admin/blocks',              4),
    ('media:list',                   '/admin/media',               6),
    ('content:list',                 '/admin/articles',            1),
    ('page:site_slot_list',          '/admin/site-slots',          3),
    ('i18n:manage',                  '/admin/i18n',                8),
    ('product:list',                 '/admin/products',            1),
    ('product:attribute_list',       '/admin/product-attributes',  2),
    ('product:category_list',        '/admin/product-categories',  3),
    ('product:brand_list',           '/admin/product-brands',      4),
    ('product:tag_list',             '/admin/product-tags',        5),
    ('product:pricing_rules',        '/admin/product-pricing',     6),
    ('product:bundle_get',           '/admin/products/bundle',     7),
    ('inventory:warehouse_list',     '/admin/inventory',           9),
    ('inventory:source_list',        '/admin/inventory/sources',   10),
    ('inventory:purchase_list',      '/admin/inventory/purchases', 11),
    ('masterdata:change_list',       '/admin/masterdata/changes',  12),
    ('order:list',                   '/admin/orders',              1),
    ('order:coupon_list',            '/admin/coupons',             3),
    ('user:customer_list',           '/admin/customers',           4),
    ('analytics:view',               '/admin/analytics',           4),
    ('mail:account_list',            '/admin/mail',                1)
) AS v(code, path, sort)
WHERE m.permission_code = v.code AND m.type = 2 AND m.deleted_at IS NULL;

-- ---- 3. 归位：按路径挂到所属分组目录下（路径唯一，比 code 更适合做归位键） ----

UPDATE sys_menus m
SET parent_id = COALESCE((
        SELECT p.id FROM sys_menus p
        WHERE p.title = v.parent AND p.type = 1 AND p.deleted_at IS NULL
    ), 0),
    update_time = NOW()
FROM (VALUES
    ('/admin/articles',            '内容'),
    ('/admin/pages',               '内容'),
    ('/admin/site-slots',          '内容'),
    ('/admin/blocks',              '内容'),
    ('/admin/content-templates',   '内容'),
    ('/admin/media',               '内容'),
    ('/admin/navigations',         '内容'),
    ('/admin/i18n',                '内容'),
    ('/admin/products',            '商品与库存'),
    ('/admin/product-attributes',  '商品与库存'),
    ('/admin/product-categories',  '商品与库存'),
    ('/admin/product-brands',      '商品与库存'),
    ('/admin/product-tags',        '商品与库存'),
    ('/admin/product-pricing',     '商品与库存'),
    ('/admin/products/bundle',     '商品与库存'),
    ('/admin/products/template',   '商品与库存'),
    ('/admin/inventory',           '商品与库存'),
    ('/admin/inventory/sources',   '商品与库存'),
    ('/admin/inventory/purchases', '商品与库存'),
    ('/admin/masterdata/changes',  '商品与库存'),
    ('/admin/orders',              '交易'),
    ('/admin/returns',             '交易'),
    ('/admin/coupons',             '交易'),
    ('/admin/customers',           '交易'),
    ('/admin/themes',              '站点'),
    ('/admin/settings',            '站点'),
    ('/admin/seo',                 '站点'),
    ('/admin/analytics',           '站点'),
    ('/api/page/redirect',         '站点'),
    ('/admin/mail',                '系统'),
    ('/admin/mail/marketing',      '系统'),
    ('/admin/mail/campaign',       '系统'),
    ('/admin/mail/automation',     '系统'),
    ('/admin/plugins',             '系统')
) AS v(path, parent)
WHERE m.path = v.path AND m.type = 2 AND m.deleted_at IS NULL;

-- ---- 4. 补上侧栏此前够不到的页面（用路径做存在性判定） ----

INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.title,
       COALESCE((
           SELECT p.id FROM sys_menus p
           WHERE p.title = v.parent AND p.type = 1 AND p.deleted_at IS NULL
       ), 0),
       2, v.path, '', v.code, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    -- 管理：admin 六领域此前只有 API，没有任何后台入口挂在侧栏上
    ('管理员',         '管理',       '/admin/administrators',     'admin:list',            1),
    ('角色管理',       '管理',       '/admin/roles',              'role:list',             2),
    ('菜单管理',       '管理',       '/admin/menus',              'menu:list',             3),
    ('权限资源',       '管理',       '/admin/permissions',        'permission:list',       4),
    ('部门管理',       '管理',       '/admin/departments',        'dept:list',             5),
    ('数据权限',       '管理',       '/admin/datarules',          'datarule:list',         6),
    -- 内容 / 结构资产
    ('内容模板',       '内容',       '/admin/content-templates',  'contenttemplate:list',  5),
    ('导航菜单',       '内容',       '/admin/navigations',        'navigation:list',       7),
    -- 商品域
    ('商品详情模板',   '商品与库存', '/admin/products/template',  'product:get',           8),
    -- 交易
    ('退货入库',       '交易',       '/admin/returns',            'order:return_list',     2),
    -- 站点
    ('主题管理',       '站点',       '/admin/themes',             'project:theme_list',    1),
    ('站点设置',       '站点',       '/admin/settings',           'project:detail',        2),
    ('SEO 控制台',     '站点',       '/admin/seo',                'seo:audit',             3),
    -- 邮件
    ('邮件营销',       '系统',       '/admin/mail/marketing',     'mail:campaign_list',    2),
    ('邮件活动',       '系统',       '/admin/mail/campaign',      'mail:campaign_list',    3),
    ('邮件自动化',     '系统',       '/admin/mail/automation',    'mail:automation_list',  4)
) AS v(title, parent, path, code, sort)
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus m WHERE m.path = v.path AND m.type = 2 AND m.deleted_at IS NULL
);

-- ---- 5. 仪表盘：一级直接链接（无子节点）+ is_public=1 = 登录即可见，与原来的行为一致 ----

INSERT INTO sys_menus (title, parent_id, type, path, icon, is_public, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '仪表盘', 0, 2, '/admin', 'layout-dashboard', 1, 1, 0, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus WHERE path = '/admin' AND type = 2 AND deleted_at IS NULL
);

-- ---- 6. 插件管理补查看权限码 ----
--
-- 它由 032 seed 时 permission_code 留空；NULL 权限码在授权树里谁都匹配不上
-- （只有 is_public=1 才免权限），切表后这个菜单会直接消失。

UPDATE sys_menus
SET permission_code = 'plugin:list', update_time = NOW()
WHERE type = 2 AND path = '/admin/plugins' AND deleted_at IS NULL
  AND (permission_code IS NULL OR permission_code = '');

-- ---- 7. 隐藏「有授权意义、没有页面」的遗留条目 ----
--
-- 项目管理 /project、构建产物 /artifact、发布管理 /publication 都是 Vue 时代的路由，
-- 今天 dashboard 里没有对应页面。保留行（角色可能已按 menu_id 授权其权限码），
-- 只从侧栏移除；要恢复显示把 is_hidden 改回 0 即可。

UPDATE sys_menus
SET is_hidden = 1, update_time = NOW()
WHERE type = 2 AND deleted_at IS NULL
  AND permission_code IN ('project:list', 'artifact:detail', 'publication:receipts_pending');
