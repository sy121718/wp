-- 508 · 分类列表行「父级不属于本工程」徽章的词条（收尾批 · 分类列表可见性）。
--
-- 背景：父级指向别的工程（或父级行已不存在）的分类，此前在列表里与普通顶级分类长得一模一样 ——
-- 操作者看不出这一行的 ParentID 指向本工程之外，也就不会去改它。收尾批给它加了可见徽章
-- （internal/templates/admin/product/product_categories.html），文案的 key
-- `admin.product_categories.parent.out_of_scope` 同时被抽屉里的父级下拉复用
-- （internal/module/product/inbound/http/product_taxonomy_page.go 的 categoryParentText）——
-- 两处是同一句话，所以「一处定义、两处消费」，不各写一份常量。
--
-- 为什么必须同批 seed：scripts/check-i18n-keys-seeded.sh 的判据是「admin. / workbench. 命名空间里
-- 被引用、但 public/ 里没有任何 INSERT 的 key」；只加 key 不加词条会把基线顶上去，而那条基线只降不升。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；只新增、不改任何既有词条。
-- 判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量
-- （见 register_product_category_parent_i18n.go）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_categories.parent.out_of_scope', 'zh-CN', '（不属于本工程）', 200, 'admin', '商品分类：父级属于别的工程的可见提示（列表行徽章 + 抽屉父级下拉）', 1, now(), now()),
('admin.product_categories.parent.out_of_scope', 'en-US', '(other project)', 200, 'admin', '商品分类：父级属于别的工程的可见提示（列表行徽章 + 抽屉父级下拉） (en)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
