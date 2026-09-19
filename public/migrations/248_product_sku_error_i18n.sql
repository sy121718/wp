-- 248 · 商品主体 SKU（2026-09-19 评审第四轮）的词条：两条业务错误 + 新建抽屉与详情页的新文案。
--
-- 与本项目所有 enums 常量一致：常量值就是 i18n key，真文案在这张表里。缺词条的后果是页面上
-- 原样显示 ErrSkuContainerMissing 这种裸 key（既不中文也不是话），所以新增常量必须同批 seed。
-- 两条错误都是**可行动的**：一条提示显式填写主体 SKU 编码，一条提示商品 URL 段需要含 ASCII 字符。
--
-- 同时 seed 本批模板新增的三个 key（新建抽屉的字段名与 placeholder、详情页主体 SKU 的唯一性说明）：
-- 中文站点有模板兜底看起来正常，英文站点会退回中文兜底 —— 真源是本表，所以一并补上。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrSkuContainerMissing', 'zh-CN', 'SKU 编码无法生成：商品 URL 段不含 ASCII 字符，请显式填写 SKU 编码', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrSkuContainerMissing', 'en-US', 'Cannot generate an SKU code: the product URL slug contains no ASCII characters; please fill in the SKU code explicitly', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrContainerSkuInvalid', 'zh-CN', '主体 SKU 不合法：必须是非空字符串', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrContainerSkuInvalid', 'en-US', 'Invalid container SKU: it must be a non-empty string', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('admin.products.label.sku', 'zh-CN', 'SKU 编码', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.label.sku', 'en-US', 'SKU code', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.sku', 'zh-CN', '留空自动生成；填了并选择了仓库时自动加仓库码前缀', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.sku', 'en-US', 'Leave empty to generate automatically; if filled and a warehouse is selected, the warehouse code prefix is added automatically', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.product_detail.sku.scope', 'zh-CN', '在本工程内唯一（同工程的两个商品不能共用同一主体 SKU），变体 SKU 以它为主体拼接。', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now()),
('admin.product_detail.sku.scope', 'en-US', 'Unique within this project (two products in the same project cannot share one container SKU); variant SKUs are built on top of it.', 200, 'product', 'internal/templates/admin/product_detail.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
