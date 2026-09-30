-- 479 · 商品主图字段的文案。
--
-- 背景：products.default_image 一直是契约里的字段（创建/更新 DTO 都有，构建期还拿它当
--   图集为空时的兜底图），但后台编辑页从来没有对应控件 —— 运营只能靠导入或接口设它。
--   本批补上单图媒体字段，这是它的一个标签词条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 1 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.label.defaultImage', 'zh-CN', '主图', 200, 'admin', '商品主图字段', 1, now(), now()),
('admin.products.label.defaultImage', 'en-US', 'Main image', 200, 'admin', '商品主图字段', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
