-- 496_media_variant_full_slot.sql — 变体槽位改名：webp → full
--
-- 背景：该槽位自建立起产出的**一直是 JPEG**（编码统一走 image_processor.go 的
-- encodeJPEGBytes，质量 variantJPEGQuality），从不输出 WebP。类型名与产物格式不符，
-- 将来接 WebP/AVIF 输出时会把改代码的人骗一次 —— 所以在造成误解前改名。
--
-- 为什么存量行必须一起改：改名后 VariantTypes() 只产出 thumb/medium/full，
-- 而旧的 webp 行既不会被重新生成覆盖，也不再被反向对账的命名约定承认
-- （见 media_reconcile.go 的 mediaVariantRe）—— 留着就是一组
-- 「代码不认识、对账不敢归属」的僵尸行。
--
-- 幂等：UPDATE 只命中 variant_type='webp'，重跑影响 0 行；COMMENT 是覆盖写。
UPDATE sys_media_variant SET variant_type = 'full' WHERE variant_type = 'webp';

COMMENT ON TABLE sys_media_variant IS '媒体图片变体表（缩略图 / 调节尺寸版 / 全尺寸版）';
COMMENT ON COLUMN sys_media_variant.variant_type IS '变体类型：thumb/medium/full（full 槽位原名 webp，产物历来是有损 JPEG）';
