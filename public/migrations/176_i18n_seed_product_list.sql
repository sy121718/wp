-- 176 · i18n 词条 seed（商品列表组件固定文案 site.component.productList.*，审计 I18N-010）
--
-- 覆盖：16 个 key / zh-CN 16 行 / en-US 16 行（中英均为人工编写，非机器伪造）。
-- 来源：internal/builder/components/productlist/{i18n.go,product_list.jet}
--   —— 这些文案此前硬编码在模板里，会随产物烘进静态 HTML：切到英文后仍是中文，
--   而且发布后改不了（改一句要改代码再重新构建）。抽 key 之后构建期按语言取词。
-- 命名：site.component.productList.{语义}（docs/06-D §10.3）。
-- 占位符：仅 %s（与 pkg/i18n.HasStringPlaceholdersOnly 约定一致；pageCurrent 用单个 %s）——
--   «第 %s 页» 这种带计数的文案整串替换，不拼「前缀 + 数字 + 后缀」（别的语言不是这个形状）。
-- 兜底：构建期缺词条时回退组件包内中文原文（core.RenderContext.Text），绝不输出空串或裸 key。
-- 语义（审计 I18N-003）：ON CONFLICT DO NOTHING —— seed 是**默认值来源**，不是真相来源。
--   后台改过的词条不会被下一次迁移覆盖（DO UPDATE 的旧写法会让运营的修改在下次部署时
--   静默回滚，而「我明明改过」这种问题极难定位）。要改默认值请改这里的 item_value 并删除
--   对应行后重跑，或在后台直接修改。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE（可重复执行；后台人工改过的词条会被本次 seed 覆盖 ——
--   这是 seed 的既有语义，060 与 163 同样如此）。
-- 注册见 register.go：CheckSQL 以 site.component.productList.* 的 zh-CN 行数 16 为门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.component.productList.filters', 'en-US', 'Product filters', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.filters', 'zh-CN', '筛选', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.rating', 'en-US', 'Rating', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.rating', 'zh-CN', '评分', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.price', 'en-US', 'Price', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.price', 'zh-CN', '价格', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMin', 'en-US', 'Min', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMin', 'zh-CN', '最低', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMinAria', 'en-US', 'Minimum price', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMinAria', 'zh-CN', '最低价', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMax', 'en-US', 'Max', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMax', 'zh-CN', '最高', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMaxAria', 'en-US', 'Maximum price', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceMaxAria', 'zh-CN', '最高价', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceApply', 'en-US', 'Apply price', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.priceApply', 'zh-CN', '应用价格', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.sort', 'en-US', 'Sort', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.sort', 'zh-CN', '排序', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.pageSize', 'en-US', 'Per page', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.pageSize', 'zh-CN', '每页', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.view', 'en-US', 'View', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.view', 'zh-CN', '视图', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.onSale', 'en-US', 'On sale', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.onSale', 'zh-CN', '在售', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.pager', 'en-US', 'Pagination', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.pager', 'zh-CN', '分页', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.prev', 'en-US', 'Previous', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.prev', 'zh-CN', '上一页', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.next', 'en-US', 'Next', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.next', 'zh-CN', '下一页', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.pageCurrent', 'en-US', 'Page %s', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now()),
('site.component.productList.pageCurrent', 'zh-CN', '第 %s 页', 200, 'ui', 'internal/builder/components/productlist/{i18n.go,product_list.jet}', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
