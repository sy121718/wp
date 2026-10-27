-- 670b · 采购单七态状态机的词条（配合迁移 670）。
--
-- 三件事：
--
--   1. 四个新状态的文案（草稿 / 已下单 / 已到货 / 异常）+ received 独立成 key。
--      原先「已到货」复用列头词条 admin.inventory_purchases.col.received（值「已入库」），
--      那是**数量口径**（一共收进来多少件）；单据状态说的是**这张单走到哪一步**。
--      两者在七态里必须分开：一张单可以是「已到货」（状态）而某一行仍显示「已入库 3」。
--   2. 两个旧状态的措辞跟着改：partial「部分入库」→「部分到货」、finished「已关单」→「已完结」。
--      「入库」是行级动作，「到货」是整单的结果；老措辞在列表里与列头的「已入库」撞词，
--      运营看到两个一模一样的词分别指状态与数量，只能靠位置分辨。
--   3. 四个新错误（下单 / 标异常 / 解除异常 / 收货前置状态不满足）+ 三条改了措辞的旧错误。
--      旧措辞里写死了「未入库」这个已不存在的状态名 —— 提示语指向一个列表里筛不出来的值，
--      比没有提示更糟。
--
-- 幂等：ON CONFLICT DO UPDATE —— 本批有一半是**改值**（旧措辞已不成立），
-- DO NOTHING 会让存量库永远停在错的文案上（650 同因）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory_purchases.status.draft', 'zh-CN', '草稿', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.draft', 'en-US', 'Draft', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.ordered', 'zh-CN', '已下单', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.ordered', 'en-US', 'Ordered', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.partial', 'zh-CN', '部分到货', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.partial', 'en-US', 'Partially received', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.received', 'zh-CN', '已到货', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.received', 'en-US', 'Received', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.exception', 'zh-CN', '异常', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.exception', 'en-US', 'Exception', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.finished', 'zh-CN', '已完结', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.status.finished', 'en-US', 'Finished', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.create.asDraft', 'zh-CN', '存为草稿（先不下单，之后在列表里点「下单」）', 200, 'inventory', 'internal/templates/admin/purchase/purchase_create_form.html', 1, now(), now()),
('admin.inventory_purchases.create.asDraft', 'en-US', 'Save as draft (not ordered yet; place the order from the list later)', 200, 'inventory', 'internal/templates/admin/purchase/purchase_create_form.html', 1, now(), now()),
('admin.inventory_purchases.place', 'zh-CN', '下单', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.place', 'en-US', 'Place order', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.placeConfirm', 'zh-CN', '下单？下单后行就锁定，收货按这张单进行。', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.placeConfirm', 'en-US', 'Place this order? Lines are locked afterwards and receipts are posted against it.', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.exception', 'zh-CN', '标记异常', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.exception', 'en-US', 'Mark exception', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.exceptionConfirm', 'zh-CN', '标记异常？标记后暂停收货，直到解除异常。', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.exceptionConfirm', 'en-US', 'Mark this order as an exception? Receiving is paused until the exception is cleared.', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.resume', 'zh-CN', '解除异常', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.resume', 'en-US', 'Clear exception', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.resumeConfirm', 'zh-CN', '解除异常？解除后按已到数量重新判定单据状态，收货入口恢复。', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.resumeConfirm', 'en-US', 'Clear the exception? The status is re-derived from received quantities and receiving opens again.', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.receipt.submitAll', 'zh-CN', '确认收货', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.receipt.submitAll', 'en-US', 'Post receipts', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.receipt.submitHint', 'zh-CN', '本次到货默认填满剩余量；留 0 的行本次不收。', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.receipt.submitHint', 'en-US', 'Quantity now defaults to the outstanding amount; lines left at 0 are not received this time.', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.intro.tail', 'zh-CN', '由收货原子递增；单据状态里「已下单 / 部分到货 / 已到货」三个由「已入库数量 与 采购数量」推出，草稿 / 异常 / 已完结 / 已取消只由动作推进 —— 表单里没有状态字段。', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.intro.tail', 'en-US', ' is increased atomically by each receipt; among the statuses, ordered / partially received / received are derived from the received quantity against the ordered quantity, while draft / exception / finished / cancelled are only advanced by actions — the form has no status field.', 200, 'inventory', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('ErrPurchaseStatusInvalid', 'zh-CN', '状态筛选值不在合法状态内（草稿 / 已下单 / 部分到货 / 已到货 / 异常 / 已完结 / 已取消）', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusInvalid', 'en-US', 'The status filter is not one of draft / ordered / partially received / received / exception / finished / cancelled', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotFinishable', 'zh-CN', '只有已到货或部分到货的采购单可以提前关单', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotFinishable', 'en-US', 'Only received or partially received purchase orders can be closed early', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotCancelable', 'zh-CN', '只有草稿 / 已下单 / 部分到货 / 异常中的采购单可以取消（已到货的货已进仓，要走退货）', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotCancelable', 'en-US', 'Only draft / ordered / partially received / exception orders can be cancelled; fully received stock already sits in the warehouse', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotPlaceable', 'zh-CN', '只有草稿可以下单（已下单的单重复下单不改变任何东西）', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotPlaceable', 'en-US', 'Only a draft can be placed; re-placing an ordered purchase order changes nothing', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotExceptionable', 'zh-CN', '只有已下单 / 部分到货 / 已到货的采购单可以标记异常（草稿还没下单，已完结 / 已取消的单已经收尾）', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotExceptionable', 'en-US', 'Only ordered / partially received / received purchase orders can be marked as an exception (a draft is not ordered yet; finished and cancelled orders are final)', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotResumable', 'zh-CN', '只有异常中的采购单可以解除异常', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotResumable', 'en-US', 'Only an order marked as an exception can be resumed', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotReceivable', 'zh-CN', '只有已下单 / 部分到货的采购单可以登记收货（草稿还没下单；已到货 / 已完结 / 已取消已收尾；异常要先解除）', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now()),
('ErrPurchaseStatusNotReceivable', 'en-US', 'Only ordered or partially received purchase orders accept receipts (a draft is not ordered; received, finished and cancelled orders are final; clear the exception first)', 409, 'inventory', 'internal/module/purchase/enums/purchase_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET
    item_value = EXCLUDED.item_value,
    http_code = EXCLUDED.http_code,
    remark = EXCLUDED.remark,
    update_time = now();
