-- 179 · 购物车模块文案词条（审计 I18N-002）。
--
-- 模块 enums 的常量值改为 i18n key 后，真正的文案落在这里。响应层
-- pkg/response.translate 按请求语言查表；未命中原样返回 key（可见的降级，不是空白）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源，
-- 运营在后台改过的词条不会被下一次迁移覆盖。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('cart.msg.updated', 'zh-CN', '购物车已更新', 'cart', '', 1, 200, now(), now()),
    ('cart.msg.updated', 'en-US', 'Cart updated', 'cart', '', 1, 200, now(), now()),
    ('cart.msg.cleared', 'zh-CN', '购物车已清空', 'cart', '', 1, 200, now(), now()),
    ('cart.msg.cleared', 'en-US', 'Cart cleared', 'cart', '', 1, 200, now(), now()),
    ('cart.err.invalidParam', 'zh-CN', '参数不合法', 'cart', '', 1, 200, now(), now()),
    ('cart.err.invalidParam', 'en-US', 'Invalid parameter', 'cart', '', 1, 200, now(), now()),
    ('cart.err.projectRequired', 'zh-CN', '缺少站点工程', 'cart', '', 1, 200, now(), now()),
    ('cart.err.projectRequired', 'en-US', 'Site project is required', 'cart', '', 1, 200, now(), now()),
    ('cart.err.variantRequired', 'zh-CN', '缺少商品规格', 'cart', '', 1, 200, now(), now()),
    ('cart.err.variantRequired', 'en-US', 'Product variant is required', 'cart', '', 1, 200, now(), now()),
    ('cart.err.quantityInvalid', 'zh-CN', '商品数量必须为正整数', 'cart', '', 1, 200, now(), now()),
    ('cart.err.quantityInvalid', 'en-US', 'Quantity must be a positive integer', 'cart', '', 1, 200, now(), now()),
    ('cart.err.quantityTooMany', 'zh-CN', '单件商品的数量超出上限', 'cart', '', 1, 200, now(), now()),
    ('cart.err.quantityTooMany', 'en-US', 'Quantity exceeds the per-item limit', 'cart', '', 1, 200, now(), now()),
    ('cart.err.cartEmpty', 'zh-CN', '购物车是空的', 'cart', '', 1, 200, now(), now()),
    ('cart.err.cartEmpty', 'en-US', 'Your cart is empty', 'cart', '', 1, 200, now(), now()),
    ('cart.err.cartFull', 'zh-CN', '购物车里放不下更多商品了，请先结算或清空', 'cart', '', 1, 200, now(), now()),
    ('cart.err.cartFull', 'en-US', 'Your cart cannot hold more items; please check out or clear it', 'cart', '', 1, 200, now(), now()),
    ('cart.err.cartItemAbsent', 'zh-CN', '购物车里没有这件商品', 'cart', '', 1, 200, now(), now()),
    ('cart.err.cartItemAbsent', 'en-US', 'This item is not in your cart', 'cart', '', 1, 200, now(), now()),
    ('cart.err.variantNotFound', 'zh-CN', '商品规格不存在或已下架', 'cart', '', 1, 200, now(), now()),
    ('cart.err.variantNotFound', 'en-US', 'Product variant not found or no longer available', 'cart', '', 1, 200, now(), now()),
    ('cart.err.outOfStock', 'zh-CN', '库存不足，无法下单', 'cart', '', 1, 200, now(), now()),
    ('cart.err.outOfStock', 'en-US', 'Out of stock', 'cart', '', 1, 200, now(), now()),
    ('cart.err.emailRequired', 'zh-CN', '请填写邮箱，订单与账号信息会发到这里', 'cart', '', 1, 200, now(), now()),
    ('cart.err.emailRequired', 'en-US', 'Email is required so we can send your order and account details', 'cart', '', 1, 200, now(), now()),
    ('cart.err.emailInvalid', 'zh-CN', '邮箱格式不正确', 'cart', '', 1, 200, now(), now()),
    ('cart.err.emailInvalid', 'en-US', 'Invalid email address', 'cart', '', 1, 200, now(), now()),
    ('cart.err.nameRequired', 'zh-CN', '请填写收货人姓名', 'cart', '', 1, 200, now(), now()),
    ('cart.err.nameRequired', 'en-US', 'Recipient name is required', 'cart', '', 1, 200, now(), now()),
    ('cart.err.phoneRequired', 'zh-CN', '请填写联系电话', 'cart', '', 1, 200, now(), now()),
    ('cart.err.phoneRequired', 'en-US', 'Contact phone is required', 'cart', '', 1, 200, now(), now()),
    ('cart.err.addressRequired', 'zh-CN', '请填写收货地址', 'cart', '', 1, 200, now(), now()),
    ('cart.err.addressRequired', 'en-US', 'Shipping address is required', 'cart', '', 1, 200, now(), now()),
    ('cart.err.paymentFailed', 'zh-CN', '支付未成功，订单已创建，请稍后在订单中继续支付', 'cart', '', 1, 200, now(), now()),
    ('cart.err.paymentFailed', 'en-US', 'Payment did not go through; your order was created, please pay later from your orders', 'cart', '', 1, 200, now(), now()),
    ('cart.err.callbackSignature', 'zh-CN', '回调签名校验失败', 'cart', '', 1, 200, now(), now()),
    ('cart.err.callbackSignature', 'en-US', 'Callback signature verification failed', 'cart', '', 1, 200, now(), now()),
    ('cart.err.callbackOrderMissing', 'zh-CN', '回调对应的订单不存在', 'cart', '', 1, 200, now(), now()),
    ('cart.err.callbackOrderMissing', 'en-US', 'Order referenced by the callback was not found', 'cart', '', 1, 200, now(), now()),
    ('cart.err.callbackAmountMismatch', 'zh-CN', '回调金额与订单金额不一致', 'cart', '', 1, 200, now(), now()),
    ('cart.err.callbackAmountMismatch', 'en-US', 'Callback amount does not match the order total', 'cart', '', 1, 200, now(), now()),
    ('cart.err.internal', 'zh-CN', '操作失败，请稍后重试', 'cart', '', 1, 200, now(), now()),
    ('cart.err.internal', 'en-US', 'Something went wrong, please try again later', 'cart', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
