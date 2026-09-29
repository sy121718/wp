-- 构建期组件文案的词条（core.addToCart / core.cartIcon 的 Go 侧与模板硬编码收口）。
--
-- 这些文案会**烘进静态产物**：构建期组件渲染出的字节就是访客最终拿到的那份 HTML，
-- 所以它们必须按站点语言在构建期取词 —— 访客请求不执行模板、不查库，发布后也改不了。
--
-- 收口的六处（此前都是硬编码中文，英文站点上是中文）：
--   · addToCart：数量输入框的 aria-label（读屏器念的，页面上看不到）
--   · addToCart：两条降级提示（缺站点工程 / 商品暂无可购买规格）
--   · cartIcon：浮层占位文案「正在加载购物车…」与其中的「查看购物车」兜底链接
--   · cartIcon：缺站点工程时的降级提示
--
-- 词条 key 沿用 site.component.{组件}.{语义}；不新造同义 key ——
-- 按钮文字等既有词条（site.component.addToCart.button / site.component.cartIcon.label）不动。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.component.addToCart.qtyAria', 'zh-CN', '数量', 200, 'ui', 'internal/builder/components/addtocart/{jet.go,add_to_cart.jet}', 1, now(), now()),
('site.component.addToCart.qtyAria', 'en-US', 'Quantity', 200, 'ui', 'internal/builder/components/addtocart/{jet.go,add_to_cart.jet}', 1, now(), now()),
('site.component.addToCart.notice.noProject', 'zh-CN', '加购暂不可用（未取到站点工程）', 200, 'ui', 'internal/builder/components/addtocart/{jet.go,add_to_cart.jet}', 1, now(), now()),
('site.component.addToCart.notice.noProject', 'en-US', 'Add to cart unavailable (site project not resolved)', 200, 'ui', 'internal/builder/components/addtocart/{jet.go,add_to_cart.jet}', 1, now(), now()),
('site.component.addToCart.notice.noVariant', 'zh-CN', '暂无可购买的规格', 200, 'ui', 'internal/builder/components/addtocart/{jet.go,add_to_cart.jet}', 1, now(), now()),
('site.component.addToCart.notice.noVariant', 'en-US', 'No variant is available for purchase right now', 200, 'ui', 'internal/builder/components/addtocart/{jet.go,add_to_cart.jet}', 1, now(), now()),
('site.component.cartIcon.loading', 'zh-CN', '正在加载购物车…', 200, 'ui', 'internal/builder/components/carticon/{jet.go,cart_icon.jet}', 1, now(), now()),
('site.component.cartIcon.loading', 'en-US', 'Loading your cart…', 200, 'ui', 'internal/builder/components/carticon/{jet.go,cart_icon.jet}', 1, now(), now()),
('site.component.cartIcon.viewCart', 'zh-CN', '查看购物车', 200, 'ui', 'internal/builder/components/carticon/{jet.go,cart_icon.jet}', 1, now(), now()),
('site.component.cartIcon.viewCart', 'en-US', 'View cart', 200, 'ui', 'internal/builder/components/carticon/{jet.go,cart_icon.jet}', 1, now(), now()),
('site.component.cartIcon.notice', 'zh-CN', '购物车暂不可用（未取到站点工程）', 200, 'ui', 'internal/builder/components/carticon/{jet.go,cart_icon.jet}', 1, now(), now()),
('site.component.cartIcon.notice', 'en-US', 'Cart unavailable (site project not resolved)', 200, 'ui', 'internal/builder/components/carticon/{jet.go,cart_icon.jet}', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
