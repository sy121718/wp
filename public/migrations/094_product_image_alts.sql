-- 094 · 商品图集 alt 文本列（issue #12 商品多语言）
--
-- 商品图集（products.images）在产物里由 core.gallery / core.product 输出为 <img>；
-- alt 是无障碍与 SEO 的必填属性。原实现用商品名兜底 alt，商品名可翻译，但
-- 「什么图配什么 alt」是作者填写的文本，必须能独立翻译（issue #12 验收 1：
-- 商品名、副标题、描述、图片 alt 可按语言翻译）。
--
-- 形状：与 images 逐位对应的 JSON 数组（第 i 个元素是第 i 张图的 alt；空串合法，
-- 表示该图仍是装饰性图片，产物由商品名兜底）。缺列/空数组都退化为旧行为。
--
-- 翻译：alt 是作者文本，进 sys_translation（语境 product.imageAlts，逐元素取词）；
-- URL 本身（product.images）永不翻译。
--
-- 注册：public/migrations/register.go（Migration 094-product-image-alts）。

ALTER TABLE products ADD COLUMN IF NOT EXISTS images_alt jsonb NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN products.images_alt IS '图集 alt 文本数组（与 images 逐位对应；元素可为空串；可翻译字段，语境 product.imageAlts）';
