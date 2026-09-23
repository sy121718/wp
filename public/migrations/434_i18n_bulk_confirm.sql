-- 434 · 批量操作条（bulk bar）的按钮与确认文案（36 key × 2 语言）
--
-- 背景：后台列表页的批量操作条由 admin/partials/bulk_bar.html 提供结构，
--   **取词留在调用点**（该片段的注释写明：片段只收「结构 + 由调用点传进来的成品文案」，
--   否则 admin_group_f_i18n_test.go 的 key 判据会把词条算成孤儿）。于是每个列表页
--   自己写 tr("admin.xxx.bulkDeleteConfirm", "…") 之类的确认文案。
--
--   本轮全量扫描暴露的问题：这些 key 在 sys_i18n 里**大面积不存在**。
--   模板的取词写法是文件顶部 {{tr := .["t"]}} 声明一次、range 内复用 {{tr("key", "兜底")}} ——
--   此前用只匹配 {{ .["t"]("key", …) }} 直调形式的正则扫描时，这批 key **整批漏掉**，
--   于是「库里没有 → 英文界面回落模板里的中文兜底」这个缺陷一直没被看见。
--   本批补齐：凡是模板在用、库里没有的批量类词条，一律纳入。
--
--   为什么按 zh-CN 兜底逐字照抄：库值存在时**覆盖**模板兜底（t() 的语义是 key 命中取库值），
--   两边不一致就等于顺手改了文案。本文件所有 zh-CN 值都是从模板里逐字抠出来的。
--
-- 与 400 的关系：本批不含 filter_empty 类（那两条由 400 负责，见 register_client_filter_empty_i18n.go）。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（sys_i18n 主键是 (item_key, lang)），
--   重复执行影响 0 行。本批只新增、不修改任何既有词条，故无 UPDATE。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.article.list.bulkDeleteConfirm', 'zh-CN', '删除选中的文章？引用它们的页面会在下次构建时失去内容。', 200, 'admin', 'admin/content/articles.html: 批量条删除确认', 1, now(), now()),
    ('admin.article.list.bulkDeleteConfirm', 'en-US', 'Delete the selected articles? Pages that reference them lose their content on the next build.', 200, 'admin', 'admin/content/articles.html: 批量条删除确认', 1, now(), now()),
    ('admin.blocks.bulk_delete_confirm', 'zh-CN', '删除选中的块？被页面引用的全局块需先解除引用，其余照常删除。', 200, 'admin', 'admin/block/blocks.html: 批量条删除确认', 1, now(), now()),
    ('admin.blocks.bulk_delete_confirm', 'en-US', 'Delete the selected blocks? Global blocks still referenced by pages must be unreferenced first; the rest are deleted as usual.', 200, 'admin', 'admin/block/blocks.html: 批量条删除确认', 1, now(), now()),
    ('admin.common.bulk.column', 'zh-CN', '选择', 200, 'admin', 'admin/order/coupons.html: 批量列表头「选择」列', 1, now(), now()),
    ('admin.common.bulk.column', 'en-US', 'Select', 200, 'admin', 'admin/order/coupons.html: 批量列表头「选择」列', 1, now(), now()),
    ('admin.content.templates.bulkDeleteConfirm', 'zh-CN', '删除选中的模板？删除会连带清掉它的历史版本；被页面、实例或其它模板引用的模板不会被删除。', 200, 'admin', 'admin/contenttemplate/content_templates.html: 批量条删除确认', 1, now(), now()),
    ('admin.content.templates.bulkDeleteConfirm', 'en-US', 'Delete the selected templates? Their version history goes with them; templates still referenced by pages, instances or other templates are kept.', 200, 'admin', 'admin/contenttemplate/content_templates.html: 批量条删除确认', 1, now(), now()),
    ('admin.coupons.bulk.delete', 'zh-CN', '批量删除', 200, 'admin', 'admin/order/coupons.html: 批量条「批量删除」按钮', 1, now(), now()),
    ('admin.coupons.bulk.delete', 'en-US', 'Delete selected', 200, 'admin', 'admin/order/coupons.html: 批量条「批量删除」按钮', 1, now(), now()),
    ('admin.coupons.bulkDeleteConfirm', 'zh-CN', '删除选中的优惠码？有核销记录的会被拒绝（请改用停用），其余照常删除。', 200, 'admin', 'admin/order/coupons.html: 批量条删除确认', 1, now(), now()),
    ('admin.coupons.bulkDeleteConfirm', 'en-US', 'Delete the selected coupons? Ones that already have redemptions are refused (disable them instead); the rest are deleted as usual.', 200, 'admin', 'admin/order/coupons.html: 批量条删除确认', 1, now(), now()),
    ('admin.coupons.bulk.target', 'zh-CN', '目标状态', 200, 'admin', 'admin/order/coupons.html: 批量条「目标状态」下拉标签', 1, now(), now()),
    ('admin.coupons.bulk.target', 'en-US', 'Target status', 200, 'admin', 'admin/order/coupons.html: 批量条「目标状态」下拉标签', 1, now(), now()),
    ('admin.coupons.bulk.toggle', 'zh-CN', '批量启停', 200, 'admin', 'admin/order/coupons.html: 批量条「批量启停」按钮', 1, now(), now()),
    ('admin.coupons.bulk.toggle', 'en-US', 'Enable / disable selected', 200, 'admin', 'admin/order/coupons.html: 批量条「批量启停」按钮', 1, now(), now()),
    ('admin.customers.bulk.disable', 'zh-CN', '批量停用', 200, 'admin', 'admin/user/customers.html: 批量条「批量停用」按钮', 1, now(), now()),
    ('admin.customers.bulk.disable', 'en-US', 'Disable selected', 200, 'admin', 'admin/user/customers.html: 批量条「批量停用」按钮', 1, now(), now()),
    ('admin.customers.bulk.disableConfirm', 'zh-CN', '停用选中的客户？停用后他们立刻登不上去，且不会自动恢复。', 200, 'admin', 'admin/user/customers.html: 批量停用确认', 1, now(), now()),
    ('admin.customers.bulk.disableConfirm', 'en-US', 'Disable the selected customers? They cannot sign in immediately after, and the state does not lift by itself.', 200, 'admin', 'admin/user/customers.html: 批量停用确认', 1, now(), now()),
    ('admin.customers.bulk.enable', 'zh-CN', '批量启用', 200, 'admin', 'admin/user/customers.html: 批量条「批量启用」按钮', 1, now(), now()),
    ('admin.customers.bulk.enable', 'en-US', 'Enable selected', 200, 'admin', 'admin/user/customers.html: 批量条「批量启用」按钮', 1, now(), now()),
    ('admin.customers.bulk.enableConfirm', 'zh-CN', '启用选中的客户？启用后他们可以重新登录。', 200, 'admin', 'admin/user/customers.html: 批量启用确认', 1, now(), now()),
    ('admin.customers.bulk.enableConfirm', 'en-US', 'Enable the selected customers? They can sign in again afterwards.', 200, 'admin', 'admin/user/customers.html: 批量启用确认', 1, now(), now()),
    ('admin.customers.bulk.unlock', 'zh-CN', '批量解除锁定', 200, 'admin', 'admin/user/customers.html: 批量条「批量解除锁定」按钮', 1, now(), now()),
    ('admin.customers.bulk.unlock', 'en-US', 'Unlock selected', 200, 'admin', 'admin/user/customers.html: 批量条「批量解除锁定」按钮', 1, now(), now()),
    ('admin.customers.bulk.unlockConfirm', 'zh-CN', '解除选中客户的登录锁定？只清登录失败计数与锁定时间，不改账号状态。', 200, 'admin', 'admin/user/customers.html: 批量解除锁定确认', 1, now(), now()),
    ('admin.customers.bulk.unlockConfirm', 'en-US', 'Unlock the selected customers? Only the failure count and the lock time are cleared — the account status is untouched.', 200, 'admin', 'admin/user/customers.html: 批量解除锁定确认', 1, now(), now()),
    ('admin.inventory_sources.bulkDeleteConfirm', 'zh-CN', '删除选中的货源？此操作不可撤销；仍被采购单或历史流水引用的会被跳过。', 200, 'admin', 'admin/inventory/inventory_sources.html: 批量条删除确认', 1, now(), now()),
    ('admin.inventory_sources.bulkDeleteConfirm', 'en-US', 'Delete the selected sources? This cannot be undone; ones still referenced by purchase orders or history are skipped.', 200, 'admin', 'admin/inventory/inventory_sources.html: 批量条删除确认', 1, now(), now()),
    ('admin.inventory.warehouses.bulkDeleteConfirm', 'zh-CN', '删除选中的仓库？默认仓或仓内仍有非零库存的会被跳过，其余照常删除。', 200, 'admin', 'admin/inventory/inventory_warehouses.html: 批量条删除确认', 1, now(), now()),
    ('admin.inventory.warehouses.bulkDeleteConfirm', 'en-US', 'Delete the selected warehouses? The default warehouse, or one that still holds non-zero stock, is skipped; the rest are deleted as usual.', 200, 'admin', 'admin/inventory/inventory_warehouses.html: 批量条删除确认', 1, now(), now()),
    ('admin.mail.accounts.bulk_delete_confirm', 'zh-CN', '删除选中的发信账号？此操作不可撤销。', 200, 'admin', 'admin/mail/mail.html: 发信账号批量条删除确认', 1, now(), now()),
    ('admin.mail.accounts.bulk_delete_confirm', 'en-US', 'Delete the selected mail accounts? This cannot be undone.', 200, 'admin', 'admin/mail/mail.html: 发信账号批量条删除确认', 1, now(), now()),
    ('admin.mail.marketing.bulk_status_confirm', 'zh-CN', '把选中的联系人改为所选状态？改成「退订」会同时写入抑制名单，之后不会再被群发到。', 200, 'admin', 'admin/mail/mail_marketing.html: 联系人批量改状态确认', 1, now(), now()),
    ('admin.mail.marketing.bulk_status_confirm', 'en-US', 'Set the selected contacts to the chosen status? Switching to Unsubscribed also writes them to the suppression list, so no later campaign reaches them.', 200, 'admin', 'admin/mail/mail_marketing.html: 联系人批量改状态确认', 1, now(), now()),
    ('admin.mail.marketing.bulk_status_submit', 'zh-CN', '批量改状态', 200, 'admin', 'admin/mail/mail_marketing.html: 联系人批量条「批量改状态」按钮', 1, now(), now()),
    ('admin.mail.marketing.bulk_status_submit', 'en-US', 'Change status', 200, 'admin', 'admin/mail/mail_marketing.html: 联系人批量条「批量改状态」按钮', 1, now(), now()),
    ('admin.mail.marketing.campaigns.bulk_delete_confirm', 'zh-CN', '删除选中的活动？投递记录与报表会一并消失；正在发送的不会被删除。', 200, 'admin', 'admin/mail/mail_marketing.html: 活动批量条删除确认', 1, now(), now()),
    ('admin.mail.marketing.campaigns.bulk_delete_confirm', 'en-US', 'Delete the selected campaigns? Their delivery records and reports go too; campaigns that are sending right now are kept.', 200, 'admin', 'admin/mail/mail_marketing.html: 活动批量条删除确认', 1, now(), now()),
    ('admin.mail.templates.bulk_delete_confirm', 'zh-CN', '删除选中的邮件模板？用到它们的邮件会发不出去。', 200, 'admin', 'admin/mail/mail.html: 邮件模板批量条删除确认', 1, now(), now()),
    ('admin.mail.templates.bulk_delete_confirm', 'en-US', 'Delete the selected mail templates? Mail that uses them will fail to send.', 200, 'admin', 'admin/mail/mail.html: 邮件模板批量条删除确认', 1, now(), now()),
    ('admin.orders.bulk.cancel', 'zh-CN', '批量取消', 200, 'admin', 'admin/order/orders.html: 批量条「批量取消」按钮', 1, now(), now()),
    ('admin.orders.bulk.cancel', 'en-US', 'Cancel selected', 200, 'admin', 'admin/order/orders.html: 批量条「批量取消」按钮', 1, now(), now()),
    ('admin.orders.bulkCancelConfirm', 'zh-CN', '取消选中的订单？尚未发货的那部分库存会归还，已发货 / 已完成的会被跳过。', 200, 'admin', 'admin/order/orders.html: 批量取消确认', 1, now(), now()),
    ('admin.orders.bulkCancelConfirm', 'en-US', 'Cancel the selected orders? Stock for the part that has not shipped is returned; shipped / completed ones are skipped.', 200, 'admin', 'admin/order/orders.html: 批量取消确认', 1, now(), now()),
    ('admin.orders.bulk.status', 'zh-CN', '批量流转', 200, 'admin', 'admin/order/orders.html: 批量条「批量流转」按钮', 1, now(), now()),
    ('admin.orders.bulk.status', 'en-US', 'Advance status', 200, 'admin', 'admin/order/orders.html: 批量条「批量流转」按钮', 1, now(), now()),
    ('admin.orders.bulk.target', 'zh-CN', '目标状态', 200, 'admin', 'admin/order/orders.html: 批量条「目标状态」下拉标签', 1, now(), now()),
    ('admin.orders.bulk.target', 'en-US', 'Target status', 200, 'admin', 'admin/order/orders.html: 批量条「目标状态」下拉标签', 1, now(), now()),
    ('admin.orders.ph.bulk_remark', 'zh-CN', '备注（可选；批量取消时作为取消原因，必填）', 200, 'admin', 'admin/order/orders.html: 批量条备注占位符', 1, now(), now()),
    ('admin.orders.ph.bulk_remark', 'en-US', 'Note (optional; required as the cancellation reason when bulk cancelling)', 200, 'admin', 'admin/order/orders.html: 批量条备注占位符', 1, now(), now()),
    ('admin.pages.bulk_delete_confirm', 'zh-CN', '删除选中的页面？页面已发布的路径会一并从线上下线（旧链接直接 404），草稿与产物一并删除。', 200, 'admin', 'admin/page/pages.html: 批量条删除确认', 1, now(), now()),
    ('admin.pages.bulk_delete_confirm', 'en-US', 'Delete the selected pages? Their published paths go offline with them (old links return 404), and drafts and artifacts are deleted too.', 200, 'admin', 'admin/page/pages.html: 批量条删除确认', 1, now(), now()),
    ('admin.product_attributes.bulkDeleteConfirm', 'zh-CN', '删除选中的属性组及其全部属性值？被商品引用的会被拒绝，其余照常删除。', 200, 'admin', 'admin/product/product_attributes.html: 批量条删除确认', 1, now(), now()),
    ('admin.product_attributes.bulkDeleteConfirm', 'en-US', 'Delete the selected attribute groups and all their values? Ones referenced by products are refused; the rest are deleted as usual.', 200, 'admin', 'admin/product/product_attributes.html: 批量条删除确认', 1, now(), now()),
    ('admin.product_brands.bulkDeleteConfirm', 'zh-CN', '删除选中的品牌？被商品引用的会被拒绝，其余照常删除。', 200, 'admin', 'admin/product/product_brands.html: 批量条删除确认', 1, now(), now()),
    ('admin.product_brands.bulkDeleteConfirm', 'en-US', 'Delete the selected brands? Ones referenced by products are refused; the rest are deleted as usual.', 200, 'admin', 'admin/product/product_brands.html: 批量条删除确认', 1, now(), now()),
    ('admin.products.bulkDeleteConfirm', 'zh-CN', '删除选中的商品及其全部变体？', 200, 'admin', 'admin/product/products.html: 批量条删除确认（片段只收成品文案，取词在调用点）', 1, now(), now()),
    ('admin.products.bulkDeleteConfirm', 'en-US', 'Delete the selected products and all their variants?', 200, 'admin', 'admin/product/products.html: 批量条删除确认（片段只收成品文案，取词在调用点）', 1, now(), now()),
    ('admin.product_tags.bulkDeleteConfirm', 'zh-CN', '删除选中的标签？商品上的这些标签会被一并解绑。', 200, 'admin', 'admin/product/product_tags.html: 批量条删除确认', 1, now(), now()),
    ('admin.product_tags.bulkDeleteConfirm', 'en-US', 'Delete the selected tags? They are unbound from the products that carry them.', 200, 'admin', 'admin/product/product_tags.html: 批量条删除确认', 1, now(), now()),
    ('admin.redirect.bulk_delete_confirm', 'zh-CN', '删除选中的重定向？它们指向的旧路径会直接返回 404（页面与产物不受影响，可以再手动加回来）。', 200, 'admin', 'admin/page/page_redirects.html: 批量条删除确认', 1, now(), now()),
    ('admin.redirect.bulk_delete_confirm', 'en-US', 'Delete the selected redirects? The old paths they cover return 404 (pages and artifacts are unaffected, and you can add them back by hand).', 200, 'admin', 'admin/page/page_redirects.html: 批量条删除确认', 1, now(), now()),
    ('admin.returns.bulk.approve', 'zh-CN', '批量同意', 200, 'admin', 'admin/order/returns.html: 批量条「批量同意」按钮', 1, now(), now()),
    ('admin.returns.bulk.approve', 'en-US', 'Approve selected', 200, 'admin', 'admin/order/returns.html: 批量条「批量同意」按钮', 1, now(), now()),
    ('admin.returns.bulk.reject', 'zh-CN', '批量拒绝', 200, 'admin', 'admin/order/returns.html: 批量条「批量拒绝」按钮', 1, now(), now()),
    ('admin.returns.bulk.reject', 'en-US', 'Reject selected', 200, 'admin', 'admin/order/returns.html: 批量条「批量拒绝」按钮', 1, now(), now()),
    ('admin.returns.bulkRejectConfirm', 'zh-CN', '拒绝选中的退货申请？客户会看到拒绝结果，拒绝理由请先填在备注框里。', 200, 'admin', 'admin/order/returns.html: 批量拒绝确认', 1, now(), now()),
    ('admin.returns.bulkRejectConfirm', 'en-US', 'Reject the selected return requests? The customer sees the rejection, so put the reason in the note field first.', 200, 'admin', 'admin/order/returns.html: 批量拒绝确认', 1, now(), now()),
    ('admin.returns.ph.bulk_remark', 'zh-CN', '备注（同意时可选；批量拒绝时作为拒绝理由，必填）', 200, 'admin', 'admin/order/returns.html: 批量条备注占位符', 1, now(), now()),
    ('admin.returns.ph.bulk_remark', 'en-US', 'Note (optional when approving; required as the rejection reason when bulk rejecting)', 200, 'admin', 'admin/order/returns.html: 批量条备注占位符', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
