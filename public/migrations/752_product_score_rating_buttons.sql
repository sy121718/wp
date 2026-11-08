-- 752 · 商品评分与 SEO 评分的按钮节点（type=3，title_key = 按钮码）。
--
-- 背景：模板里的按钮显隐从权限码改成按钮码（isisset(.Buttons["product.seo_score"])），
-- 按钮码必须是 sys_menus 里 type=3 节点的 title_key。这五个权限点此前只有权限目录行
-- （743 已补 sys_permission），没有承载它们的菜单节点 —— 模板写不出能命中的按钮码。
--
-- 与 589 同构：父菜单取「商品管理」/「商品分类」/「商品品牌」的列表菜单，
-- 找不到父节点时落顶级（0），测试精简 schema 也能量化。
--
-- 幂等：按 permission_code 判重；重跑只补缺，不覆盖已有节点。
INSERT INTO sys_menus (title_key, title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT v.code, v.title,
       COALESCE((SELECT id FROM sys_menus WHERE permission_code = v.parent AND type = 2 AND deleted_at IS NULL LIMIT 1), 0),
       3, '', '', v.perm, 1, v.sort, 0, NOW(), 0, NOW()
FROM (VALUES
    ('product.seo_score',          '商品 SEO 评分',   'product:list',           'product:seo_score',          60),
    ('product.brand_seo_score',    '品牌 SEO 评分',   'product:brand_list',     'product:brand_seo_score',    61),
    ('product.category_seo_score', '分类 SEO 评分',   'product:category_list',  'product:category_seo_score', 62),
    ('product.rating_add',         '新增商品评分',    'product:list',           'product:rating_add',         63),
    ('product.rating_delete',      '删除商品评分',    'product:list',           'product:rating_delete',      64)
) AS v(code, title, parent, perm, sort)
WHERE NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.permission_code = v.perm AND x.deleted_at IS NULL);
