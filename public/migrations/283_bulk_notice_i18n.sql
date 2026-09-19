-- 283 · 批量操作结论文案的词条（admin / order / content / product / page / inventory 六个模块的批量回执）。
--
-- 背景：这些结论文案此前是钉死在 Go 里的中文字面量（`已删除 %d 个%s` / `没有勾选任何…` /
-- `列表未改动` 这一类），写侧 Sprintf 出整句、读侧再拿**另一份**同形字面量做受控匹配。
-- 于是有两个问题同时存在：
--
--   1. 英文界面上永远显示中文 —— 回执走的是 handler 自己拼的文本（不进 response 的翻译链），
--      没有词条 = 没有翻译；
--   2. 写读各一份字面量（同一个包里的常量，或干脆各写各的），改一处措辞就会让另一处
--      静默失配：写侧把提示放进 URL，读侧判成「不是系统说过的话」而落空串 ——
--      运营看到的是「什么都没有」，且没有任何报错。
--
-- 本批把两件事一起收口：
--   · 每条文案一个词条（值 = `模块.bulk.*`），写侧与读侧共用同一份「key + 中文原文」结构体，
--     经同一个取词函数（adminBulkTextOf / orderBulkTextOf / articleBulkTextOf /
--     productBulkTextOf / pageBulkTextOf / inventoryBulkText）按当前语言取模板；
--   · 模板里的计数**一律 %s**（Go 侧 strconv.Itoa 之后再填），于是每一条都能用
--     pkg/i18n.HasStringPlaceholdersOnly 校验：词条被写坏（混进 %d）时回退中文原文，
--     而不是让 Sprintf 把参数渲染成 %!d(string=3)。
--
-- 带名词 / 动词的模板（admin 的「已删除 N 个%s」、order 的「%s %s 个%s」）**名词与动词也各是一条词条**：
-- 中英语序不同（Deleted N roles vs 已删除 N 个角色），把名词焊进模板等于按「名词 × 动词 × 分支」
-- 抄一张句子表。读侧候选同样是「模板取当前语言 + 名词取当前语言」代入后再归一。
--
-- http_code 用 200：这些是页面回执（成功态 / 部分成功态），不是错误响应。
-- category 用 ui：它们渲染在后台页面，不是 JSON 的错误消息。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
--
-- 本批共 60 个 key × 中英 = 120 行。判定见 register_bulk_notice_i18n.go（枚举本批全部 60 个 key）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    -- —— admin：六领域列表页批量删除 + 词条页批量删除 ——
    ('admin.bulk.done', 'zh-CN', '已删除 %s 个%s', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.done', 'en-US', 'Deleted %s %s(s)', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.partial', 'zh-CN', '已删除 %s 个%s，%s 个未能删除（受保护或被引用）', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.partial', 'en-US', 'Deleted %s %s(s); %s could not be deleted (protected or referenced)', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.admin', 'zh-CN', '管理员', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.admin', 'en-US', 'administrator', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.role', 'zh-CN', '角色', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.role', 'en-US', 'role', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.permission', 'zh-CN', '权限点', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.permission', 'en-US', 'permission', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.menu', 'zh-CN', '菜单', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.menu', 'en-US', 'menu', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.dept', 'zh-CN', '部门', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.dept', 'en-US', 'department', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.datarule', 'zh-CN', '数据规则', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.bulk.noun.datarule', 'en-US', 'data rule', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.noneSelected', 'zh-CN', '没有勾选任何词条，列表未改动。', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.noneSelected', 'en-US', 'No entries selected; the list is unchanged.', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.allDeleted', 'zh-CN', '已删除 %s 条词条（构建时回退到组件包内的中文兜底）。', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.allDeleted', 'en-US', 'Deleted %s entry(ies) (the build falls back to the built-in Chinese defaults).', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.allSkipped', 'zh-CN', '%s 条词条都未能删除，列表未改动。', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.allSkipped', 'en-US', 'None of the %s entry(ies) could be deleted; the list is unchanged.', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.partial', 'zh-CN', '已删除 %s 条，%s 条未能删除（可能已被删除）。', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),
    ('admin.i18nBulk.partial', 'en-US', 'Deleted %s; %s could not be deleted (possibly already deleted).', 200, 'ui', 'internal/module/admin/inbound/http/admin_err.go', 1, now(), now()),

    -- —— order：订单 / 退货申请 / 优惠码三个列表页的批量动作结论（模板 + 动词 + 名词）——
    ('order.bulk.noneSelected', 'zh-CN', '没有勾选任何%s。', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noneSelected', 'en-US', 'No %s selected.', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.allDone', 'zh-CN', '%s %s 个%s。', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.allDone', 'en-US', '%s %s %s(s).', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.allSkipped', 'zh-CN', '0 个%s%s，%s 个被跳过（状态不允许或已不存在）。', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.allSkipped', 'en-US', '0 %s %s(s), %s skipped (status not allowed or no longer exists).', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.partial', 'zh-CN', '%s %s 个%s，跳过 %s 个（状态不允许或已不存在）。', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.partial', 'en-US', '%s %s %s(s), %s skipped (status not allowed or no longer exists).', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.flowed', 'zh-CN', '已流转', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.flowed', 'en-US', 'Moved', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.cancelled', 'zh-CN', '已取消', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.cancelled', 'en-US', 'Cancelled', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.approved', 'zh-CN', '已同意', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.approved', 'en-US', 'Approved', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.rejected', 'zh-CN', '已拒绝', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.rejected', 'en-US', 'Rejected', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.deleted', 'zh-CN', '已删除', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.deleted', 'en-US', 'Deleted', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.disabled', 'zh-CN', '已停用', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.disabled', 'en-US', 'Disabled', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.enabled', 'zh-CN', '已启用', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.verb.enabled', 'en-US', 'Enabled', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noun.order', 'zh-CN', '订单', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noun.order', 'en-US', 'order', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noun.return', 'zh-CN', '退货申请', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noun.return', 'en-US', 'return request', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noun.coupon', 'zh-CN', '优惠码', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.noun.coupon', 'en-US', 'coupon', 200, 'ui', 'internal/module/order/inbound/http/order_page_query.go', 1, now(), now()),
    ('order.bulk.couponTargetInvalid', 'zh-CN', '目标状态不合法，本次没有处理任何优惠码。', 200, 'ui', 'internal/module/order/inbound/http/coupon_page_handle.go', 1, now(), now()),
    ('order.bulk.couponTargetInvalid', 'en-US', 'Invalid target status; no coupon was processed.', 200, 'ui', 'internal/module/order/inbound/http/coupon_page_handle.go', 1, now(), now()),

    -- —— content：文章列表页批量删除 ——
    ('content.bulk.noneSelected', 'zh-CN', '没有选中任何文章，列表未改动。', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.noneSelected', 'en-US', 'No articles selected; the list is unchanged.', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.allDeleted', 'zh-CN', '已删除 %s 篇文章。', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.allDeleted', 'en-US', 'Deleted %s article(s).', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.allSkipped', 'zh-CN', '%s 篇文章都未能删除，列表未改动。', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.allSkipped', 'en-US', 'None of the %s article(s) could be deleted; the list is unchanged.', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.partial', 'zh-CN', '已删除 %s 篇，%s 篇未能删除（可能已被删除）。', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),
    ('content.bulk.partial', 'en-US', 'Deleted %s; %s could not be deleted (possibly already deleted).', 200, 'ui', 'internal/module/content/inbound/http/article_handle.go', 1, now(), now()),

    -- —— product：标签 / 属性组 / 分类 / 品牌 / 商品五个批量删除 + 批量改价 + 变体清单保存 ——
    ('product.bulk.tagPartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（标签不存在或已被删除）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.tagPartial', 'en-US', 'Deleted %s; %s could not be deleted (the tag does not exist or was already deleted)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.tagDone', 'zh-CN', '已删除 %s 个标签', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.tagDone', 'en-US', 'Deleted %s tag(s)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.attrPartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（属性组不存在或被商品引用）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.attrPartial', 'en-US', 'Deleted %s; %s could not be deleted (the attribute group does not exist or is referenced by a product)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.attrDone', 'zh-CN', '已删除 %s 个属性组（连同其全部属性值）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.attrDone', 'en-US', 'Deleted %s attribute group(s) (with all of their values)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.categoryPartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（有子分类或被商品引用）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.categoryPartial', 'en-US', 'Deleted %s; %s could not be deleted (it has child categories or is referenced by a product)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.categoryDone', 'zh-CN', '已删除 %s 个分类', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.categoryDone', 'en-US', 'Deleted %s category(ies)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.brandPartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（品牌不存在或被商品引用）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.brandPartial', 'en-US', 'Deleted %s; %s could not be deleted (the brand does not exist or is referenced by a product)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.brandDone', 'zh-CN', '已删除 %s 个品牌', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.brandDone', 'en-US', 'Deleted %s brand(s)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.productPartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（商品不存在或被其它数据引用）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.productPartial', 'en-US', 'Deleted %s; %s could not be deleted (the product does not exist or is referenced by other data)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.productDone', 'zh-CN', '已删除 %s 个商品（连同其全部变体）', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.productDone', 'en-US', 'Deleted %s product(s) (with all of their variants)', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingNoneSelected', 'zh-CN', '批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。', 200, 'ui', 'internal/module/product/inbound/http/product_page_handle.go', 1, now(), now()),
    ('product.bulk.pricingNoneSelected', 'en-US', 'Bulk repricing: no product selected; tick the checkboxes on the left first.', 200, 'ui', 'internal/module/product/inbound/http/product_page_handle.go', 1, now(), now()),
    ('product.bulk.pricingNoChange', 'zh-CN', '按该规则算下来没有价格变化：%s 个商品已是目标价，未写入调价记录。', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingNoChange', 'en-US', 'No price change under this rule: %s product(s) are already at the target price; no price record was written.', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingApplied', 'zh-CN', '已按规则改价：共改 %s 个变体（%s 个商品已是目标价）。', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingApplied', 'en-US', 'Repriced by rule: %s variant(s) changed (%s product(s) already at the target price).', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingAllSkip', 'zh-CN', '没有可改价的变体：%s 个商品被跳过（%s）。', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingAllSkip', 'en-US', 'No variant can be repriced: %s product(s) skipped (%s).', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingPartial', 'zh-CN', '已改 %s 个变体，另有 %s 个商品被跳过（%s）。', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.pricingPartial', 'en-US', '%s variant(s) changed; %s product(s) skipped (%s).', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.variantSaveNoChange', 'zh-CN', '变体清单与库里一致，没有需要保存的变化。', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.variantSaveNoChange', 'en-US', 'The variant list matches the database; nothing to save.', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.variantSaveSaved', 'zh-CN', '已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.variantSaveSaved', 'en-US', 'Variant list saved: %s added, %s SKU updated, %s deleted.', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.variantSaveSkipped', 'zh-CN', '跳过 %s 个：%s', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),
    ('product.bulk.variantSaveSkipped', 'en-US', '%s skipped: %s', 200, 'ui', 'internal/module/product/inbound/http/product_err.go', 1, now(), now()),

    -- —— page：页面列表批量删除 + 重定向批量删除 ——
    ('page.bulk.pageNoneSelected', 'zh-CN', '没有勾选任何页面，列表未改动。', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageNoneSelected', 'en-US', 'No page selected; the list is unchanged.', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageAllDeleted', 'zh-CN', '已删除 %s 个页面。', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageAllDeleted', 'en-US', 'Deleted %s page(s).', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageAllSkipped', 'zh-CN', '%s 个页面都未能删除，列表未改动。', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageAllSkipped', 'en-US', 'None of the %s page(s) could be deleted; the list is unchanged.', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pagePartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（可能已被删除或路径清理失败）。', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pagePartial', 'en-US', 'Deleted %s; %s could not be deleted (possibly already deleted, or path cleanup failed).', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageMissingID', 'zh-CN', '缺少页面 id，未执行删除。', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.pageMissingID', 'en-US', 'Missing page id; nothing was deleted.', 200, 'ui', 'internal/module/page/inbound/http/page_err.go', 1, now(), now()),
    ('page.bulk.redirectNoneSelected', 'zh-CN', '没有勾选任何重定向，列表未改动。', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectNoneSelected', 'en-US', 'No redirect selected; the list is unchanged.', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectAllDeleted', 'zh-CN', '已删除 %s 条重定向。', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectAllDeleted', 'en-US', 'Deleted %s redirect(s).', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectAllSkipped', 'zh-CN', '%s 条重定向都未能删除，列表未改动。', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectAllSkipped', 'en-US', 'None of the %s redirect(s) could be deleted; the list is unchanged.', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectPartial', 'zh-CN', '已删除 %s 条，%s 条未能删除（可能已不存在或访问面不可用）。', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),
    ('page.bulk.redirectPartial', 'en-US', 'Deleted %s; %s could not be deleted (no longer exists, or the public face is unavailable).', 200, 'ui', 'internal/module/page/inbound/http/page_redirect_handle.go', 1, now(), now()),

    -- —— inventory（product/inventory）：货源页批量删除（与仓库页的 admin.inventory.bulk.* 同族）——
    ('admin.inventory.bulk.sourcePartial', 'zh-CN', '已删除 %s 个，%s 个未能删除（仍被采购单或历史流水引用）', 200, 'ui', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now()),
    ('admin.inventory.bulk.sourcePartial', 'en-US', 'Deleted %s; %s could not be deleted (still referenced by purchase orders or movement history)', 200, 'ui', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now()),
    ('admin.inventory.bulk.sourceDone', 'zh-CN', '已删除 %s 个货源', 200, 'ui', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now()),
    ('admin.inventory.bulk.sourceDone', 'en-US', 'Deleted %s source(s)', 200, 'ui', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
