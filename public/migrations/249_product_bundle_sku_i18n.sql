-- 249 · 捆绑商品主体 SKU 必填（2026-09-19 用户拍板）的词条：一条业务错误 + 新建抽屉的四个文案位。
--
-- 为什么改：上一版「捆绑未填主体 SKU → 静默按 <商品段>_B 派生」被否掉 —— 编码是商品的
-- 对外身份，必须让运营**看见并确认**，所以服务端改为必填（enums.ErrBundleSKURequired）。
-- 前台不让运营从零手打：抽屉打开 / 类型切到 bundle 时预填系统建议值，可改、可一键重新生成。
--
-- 与所有 enums 常量一致：常量值就是 i18n key，真文案在这张表里；缺词条的后果是页面上原样
-- 显示 ErrBundleSKURequired 这种裸 key（既不中文也不是话），所以新增常量必须同批 seed。
-- 模板侧的四个 key 同理：中文站点有模板兜底看起来正常，英文站点会退回中文兜底，
-- 而真源是本表。
--
-- 变体商品一字未动（丢空仍按 URL 段派生，走 ErrSkuContainerMissing / ErrContainerSkuInvalid），
-- 因此 admin.products.ph.sku 与两条旧错误词条继续有效，不退役。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrBundleSKURequired', 'zh-CN', '捆绑商品必须填写主体 SKU 编码（新建抽屉已给出建议值，可直接修改）', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrBundleSKURequired', 'en-US', 'A bundle product must have a container SKU code (the create drawer pre-fills a suggested value that you can edit)', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('admin.products.sku.regenerate', 'zh-CN', '重新生成', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.regenerate', 'en-US', 'Regenerate', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.bundleHint', 'zh-CN', '这是系统建议的唯一身份编码，可直接修改；捆绑商品的编码恒以 _B 结尾。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.bundleHint', 'en-US', 'This is the system-suggested unique identity code; edit it freely. A bundle SKU always ends with _B.', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.bundleManual', 'zh-CN', '商品名称与 URL 段都不含 ASCII 字符，派生不出建议值，请手动填写 SKU 编码。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.bundleManual', 'en-US', 'Neither the product name nor the URL slug contains ASCII characters, so no suggestion could be derived; please fill in the SKU code manually.', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.skuBundle', 'zh-CN', '系统建议值，可直接修改（捆绑商品必填，编码恒以 _B 结尾）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.skuBundle', 'en-US', 'Suggested value, editable (required for bundle products; the code always ends with _B)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
