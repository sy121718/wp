-- 合法商品暂无可买规格时保留详情页，购买按钮静态禁用。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('site.component.addToCart.unavailable', 'zh-CN', '当前商品暂不可购买'),
('site.component.addToCart.unavailable', 'en-US', 'This product is currently unavailable.')
ON CONFLICT (item_key, lang) DO NOTHING;
