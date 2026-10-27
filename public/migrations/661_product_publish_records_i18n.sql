-- 661 · 商品发布记录页（B9）的文案词条。
--
--   页面定位写在模板头注释里，这里只记三条与文案有关的口径：
--
--   1. **「没有历史」这件事必须由界面说出来**。库里没有记录每次发布成败的明细表，
--      页面只给当前状态。不说这一句，运营会把「这里没有失败记录」读成「从来没失败过」，
--      而那是两个完全不同的事实。noHistory 就是那一句。
--   2. 「待重建」不是失败：它是「线上有内容，但商品字段或模板变过，产物是旧的」。
--      staleHint 把它与「没发布成功」区分开，因为两者的处置动作不同（重建 vs 先发布）。
--   3. 「未装配」与「读不到」是两档：前者是部署侧接线缺失（重试无用），后者可重试。
--      notWired / notWiredDesc 说的是前者 —— 不写清会被人当成「一个都没发布」。
--
--   表头列名、状态三档、按钮与筛选控件优先复用既有的 admin.* 商品词条
--   （admin.products.filter.submit / .ph.keyword / admin.common.field.keyword 等，均已在
--   231 / 314 里 seed），本批只 seed 这一屏独有的 28 个 key（中英各一行，共 56 行）。
--
-- 幂等：全部是新增 key，DO NOTHING 语义 —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.publish.title', 'zh-CN', '发布记录', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.title', 'en-US', 'Publish records', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.lead', 'zh-CN', '这里看的是每个商品当前的发布状态：有没有建详情页实例、线上路径是什么、什么时候发的、产物是哪一版、要不要重建。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.lead', 'en-US', 'What you see here is each product''s current publishing state: whether a detail-page instance exists, what the live path is, when it was published, which artifact version it is, and whether it needs a rebuild.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.noHistory', 'zh-CN', '本页只给当前状态，不列发布历史 —— 系统里没有记录每次发布成败的明细表，所以不显示「上次失败」这类信息。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.noHistory', 'en-US', 'This page shows the current state only, not a publish history: the system does not keep a per-attempt success/failure log, so there is no last-failure line.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.staleHint', 'zh-CN', '「待重建」表示商品的字段或模板变过、线上还是旧产物：点「重新发布」按当前模板再发一次。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.staleHint', 'en-US', 'Needs rebuild means the product fields or template changed while the live artifact is still the old one: use Republish to publish again with the current template.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.programLead', 'zh-CN', '发布的具体动作在多语言的', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.programLead', 'en-US', 'Publishing actions live on the', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.programLink', 'zh-CN', '翻译与发布', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.programLink', 'en-US', 'translations and publishing', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.programTail', 'zh-CN', '页；模板绑定在商品的编辑页。本页与它们读同一份发布真源。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.programTail', 'en-US', 'page; template binding is on the product edit page. This page reads the same publication source.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.notWired', 'zh-CN', '发布能力还没有接入这一屏', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.notWired', 'en-US', 'Publishing capability is not wired into this page', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.notWiredDesc', 'zh-CN', '页面能打开，但发布状态的数据源没装配上来。这是部署侧的问题，稍后由维护者接线；不是「一个商品都没发布」。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.notWiredDesc', 'en-US', 'The page opens but the publication data source was not wired in. That is a deployment wiring issue for the maintainer — it does not mean no product has ever been published.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.totalNote', 'zh-CN', '共 %s 个商品', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.totalNote', 'en-US', '%s products', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.ph.keyword', 'zh-CN', '商品名称', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.ph.keyword', 'en-US', 'Product name', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.product', 'zh-CN', '商品', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.product', 'en-US', 'Product', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.site', 'zh-CN', '站点', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.site', 'en-US', 'Site', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.online', 'zh-CN', '状态', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.online', 'en-US', 'Status', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.lang', 'zh-CN', '语言与线上路径', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.lang', 'en-US', 'Language and live path', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.publishedAt', 'zh-CN', '发布时间', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.publishedAt', 'en-US', 'Published at', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.artifact', 'zh-CN', '产物版本', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.col.artifact', 'en-US', 'Artifact version', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.all', 'zh-CN', '全部状态', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.all', 'en-US', 'All statuses', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.active', 'zh-CN', '已上线', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.active', 'en-US', 'Live', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.draft', 'zh-CN', '有实例未上线', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.draft', 'en-US', 'Instance, not live', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.none', 'zh-CN', '未建实例', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.none', 'en-US', 'No instance', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.stale', 'zh-CN', '待重建', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.status.stale', 'en-US', 'Needs rebuild', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.pathUnknown', 'zh-CN', '线上路径未登记', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.pathUnknown', 'en-US', 'Live path not recorded', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.langNotPublished', 'zh-CN', '该语言还没发布成功', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.langNotPublished', 'en-US', 'This language has not been published successfully', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.noInstanceHint', 'zh-CN', '还没有详情页实例：在编辑页选一套详情页模板并发布', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.noInstanceHint', 'en-US', 'No detail-page instance yet: pick a detail-page template on the edit page and publish', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.republish', 'zh-CN', '重新发布', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.republish', 'en-US', 'Republish', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.emptyHeading', 'zh-CN', '没有符合条件的商品', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.emptyHeading', 'en-US', 'No matching products', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.emptyDesc', 'zh-CN', '当前筛选下没有商品。换个状态或清空关键词再试；商品本身在商品列表页维护。', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now()),
('admin.products.publish.emptyDesc', 'en-US', 'No products match the current filter. Try another status or clear the keyword; products themselves are maintained on the product list page.', 200, 'product', 'internal/templates/admin/product/product_publish_records.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
