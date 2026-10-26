-- 156 · i18n 词条 seed（运行时片段访客文案 site.fragment.*，I18N-012）
--
-- 覆盖：购物车 / 结算 / 库存 / 搜索 / 实时价 / 捆绑配置器 等片段 Go 侧与 jet 预翻译文案。
-- 命名：site.fragment.<能力>.<语义>（与 site.component.* 并列，均属访客面）。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.fragment.cart.empty', 'zh-CN', '购物车是空的', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.empty', 'en-US', 'Your cart is empty', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.qty_aria', 'zh-CN', '数量', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.qty_aria', 'en-US', 'Quantity', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.update', 'zh-CN', '更新', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.update', 'en-US', 'Update', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.total_prefix', 'zh-CN', '合计', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.total_prefix', 'en-US', 'Total', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.items_unit', 'zh-CN', '件', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.items_unit', 'en-US', 'items', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.checkout', 'zh-CN', '去结算', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.checkout', 'en-US', 'Checkout', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.clear', 'zh-CN', '清空购物车', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.clear', 'en-US', 'Clear cart', 200, 'ui', 'fragments/cart_view.jet', 1, now(), now()),
('site.fragment.cart.unavailable', 'zh-CN', '购物车功能尚未接入', 200, 'ui', 'runtimefragment/cart.go', 1, now(), now()),
('site.fragment.cart.unavailable', 'en-US', 'Cart is not available yet', 200, 'ui', 'runtimefragment/cart.go', 1, now(), now()),
('site.fragment.checkout.unavailable', 'zh-CN', '结算功能尚未接入', 200, 'ui', 'runtimefragment/cart.go', 1, now(), now()),
('site.fragment.checkout.unavailable', 'en-US', 'Checkout is not available yet', 200, 'ui', 'runtimefragment/cart.go', 1, now(), now()),
('site.fragment.checkout.title_paid', 'zh-CN', '下单成功', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.title_paid', 'en-US', 'Order placed', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.title_pending', 'zh-CN', '订单已创建，支付未完成', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.title_pending', 'en-US', 'Order created; payment pending', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.order_no', 'zh-CN', '订单号：', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.order_no', 'en-US', 'Order no.: ', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.total', 'zh-CN', '合计：', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.total', 'en-US', 'Total: ', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.account_mailed', 'zh-CN', '账号初始密码已发送至 %s，登录后可在账号中心查看订单。', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.checkout.account_mailed', 'en-US', 'Your account password was sent to %s. Sign in to view your orders.', 200, 'ui', 'fragments/checkout_result.jet', 1, now(), now()),
('site.fragment.stock.checkout', 'zh-CN', '以结算时库存为准', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.checkout', 'en-US', 'Stock confirmed at checkout', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.out', 'zh-CN', '暂时缺货', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.out', 'en-US', 'Out of stock', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.low', 'zh-CN', '仅剩 %d 件', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.low', 'en-US', 'Only %d left', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.in', 'zh-CN', '库存充足', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.stock.in', 'en-US', 'In stock', 200, 'ui', 'runtimefragment/variant_availability.go', 1, now(), now()),
('site.fragment.search.empty_query', 'zh-CN', '请输入搜索关键词', 200, 'ui', 'runtimefragment/search_results.go', 1, now(), now()),
('site.fragment.search.empty_query', 'en-US', 'Enter a search term', 200, 'ui', 'runtimefragment/search_results.go', 1, now(), now()),
('site.fragment.search.degraded', 'zh-CN', '搜索功能暂未接入，请稍后再试', 200, 'ui', 'runtimefragment/search_results.go', 1, now(), now()),
('site.fragment.search.degraded', 'en-US', 'Search is unavailable. Please try again later.', 200, 'ui', 'runtimefragment/search_results.go', 1, now(), now()),
('site.fragment.search.no_results', 'zh-CN', '没有找到与「%s」相关的内容', 200, 'ui', 'runtimefragment/search_results.go', 1, now(), now()),
('site.fragment.search.no_results', 'en-US', 'No results for "%s"', 200, 'ui', 'runtimefragment/search_results.go', 1, now(), now()),
('site.fragment.live_price.delisted', 'zh-CN', '该规格已下架，以结算为准', 200, 'ui', 'runtimefragment/live_price.go', 1, now(), now()),
('site.fragment.live_price.delisted', 'en-US', 'This option is unavailable; price confirmed at checkout', 200, 'ui', 'runtimefragment/live_price.go', 1, now(), now()),
('site.fragment.live_price.updated', 'zh-CN', '价格已更新为 %s，以结算为准', 200, 'ui', 'runtimefragment/live_price.go', 1, now(), now()),
('site.fragment.live_price.updated', 'en-US', 'Price updated to %s; confirmed at checkout', 200, 'ui', 'runtimefragment/live_price.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET
  item_value = EXCLUDED.item_value,
  remark = EXCLUDED.remark,
  update_time = now();
