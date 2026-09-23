-- 315 · i18n 词条 seed（商品详情页改只读：6 个 key）
--
-- 背景：商品详情页改为**只读**（写动作全部移到商品编辑页），页面上的取词随之变化 ——
--       页头悬浮说明改成「只读 + 写动作在哪」，标签区补一句「还没有标签」的空态，
--       变体表末列从「操作」改成「库存明细」（只读页没有操作），
--       编辑页补一句捆绑构成的入口说明。
--       模板兜底只在缺词条时显示中文，英文界面会回落中文，故成对 seed。
-- 覆盖：6 个 key / zh-CN 6 行 / en-US 6 行（人工编写）。
-- 语义：ON CONFLICT DO NOTHING。幂等：ConditionSQL 取本批 3 个代表 key 的 zh-CN 行数作门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_detail.hint.readonly', 'en-US', 'This page is read-only: attribute references, categories and brands, manual tags, the variant list, ratings, the SEO check and the detail-page template actions are all edited on the product edit page (reached via Edit in the product list row). This page only answers what the product is made of.', 200, 'admin', '商品详情页改只读：页头悬浮说明', 1, now(), now()),
('admin.product_detail.hint.readonly', 'zh-CN', '本页是**只读**的：属性引用、分类与品牌、手工标签、变体清单、评分、SEO 检查、详情页模板的改动都在「编辑」页（商品列表行的「编辑」进入）。这里只回答这个商品由什么组成。', 200, 'admin', '商品详情页改只读：页头悬浮说明', 1, now(), now()),
('admin.product_detail.hint.autoTags', 'en-US', 'Automatic tags are read-only - their membership is maintained by rule recalculation, so manual edits get overwritten on the next run.', 200, 'admin', '商品详情页改只读：自动标签说明', 1, now(), now()),
('admin.product_detail.hint.autoTags', 'zh-CN', '自动标签只读 —— 它的归属由规则重算维护，手工改会被下一次重算覆盖。', 200, 'admin', '商品详情页改只读：自动标签说明', 1, now(), now()),
('admin.product_detail.hint.rating', 'en-US', 'Ratings live in their own detail table: the average and count are derived from it. No ratings and rated 0 are different things - the former does not take part in minimum-rating filters.', 200, 'admin', '商品详情页改只读：评分口径说明', 1, now(), now()),
('admin.product_detail.hint.rating', 'zh-CN', '评分是独立明细：平均值与条数由明细算出；「没有评分」与「评分 0 分」是两回事，它不参与最低评分筛选。', 200, 'admin', '商品详情页改只读：评分口径说明', 1, now(), now()),
('admin.products.tags.none', 'en-US', 'This product has no tags yet.', 200, 'admin', '商品详情页只读：标签空态', 1, now(), now()),
('admin.products.tags.none', 'zh-CN', '这个商品还没有标签。', 200, 'admin', '商品详情页只读：标签空态', 1, now(), now()),
('admin.products.col.stockLink', 'en-US', 'Stock detail', 200, 'admin', '商品详情页只读：变体表末列表头（只读页没有操作列）', 1, now(), now()),
('admin.products.col.stockLink', 'zh-CN', '库存明细', 200, 'admin', '商品详情页只读：变体表末列表头（只读页没有操作列）', 1, now(), now()),
('admin.products.edit.bundleHint', 'en-US', 'Members and option rules are maintained on the bundle configuration page: members only serve as options and fulfilment detail, and member prices never take part in the bundle price.', 200, 'admin', '商品编辑页：捆绑构成入口说明', 1, now(), now()),
('admin.products.edit.bundleHint', 'zh-CN', '成员与选项规则在捆绑配置页维护：成员只作为选项与履约明细，成员价不参与套餐价。', 200, 'admin', '商品编辑页：捆绑构成入口说明', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
