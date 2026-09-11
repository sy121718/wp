-- 086 · 商品属性组与属性值（issue #7）
--
-- product_attributes 表由 081 建好（组 + 值合一，值以 JSON 承载）。本票在其上补齐
-- 「可定义属性组与值、标记是否参与变体、跨商品复用、后台可管理」四件事：
--
--   1) project_id + key 唯一：补 `key <> ''` 的部分唯一索引（081 的 uk 索引无此谓词，
--      空 key 会被当成一个可复用的重复值），并把历史行（081/085 之间写入的空 key）回填；
--   2) 参与变体标记：is_variation 已是 NOT NULL DEFAULT true，这里补显式 CHECK
--      （不设「唯一一个 is_variation 组」之类的业务约束——那是服务层规则，不是表约束）；
--   3) 权限点：属性组六条（list/get/create/update/delete/set_values），与 082 同构；
--   4) 后台菜单：菜单 type=2「商品属性」挂在「商品管理」下 + 对应按钮（type=3）。
--
-- 幂等：CREATE UNIQUE INDEX IF NOT EXISTS / 列存在判定 / NOT EXISTS 守卫。
-- 注册：public/migrations/register.go（Migration 086 + Seed 086a/086b）。

-- 0) 商品侧只存引用：products.attribute_ids 是属性组 id 数组（081 建 products 时还没有属性组）。
--    ALTER TABLE ... IF NOT EXISTS 由 SplitStatements 逐条执行，天然幂等；
--    迁移级跳过条件见 register.go（按该列是否存在判定，与 047/049/054 同一手法）。
ALTER TABLE products ADD COLUMN IF NOT EXISTS attribute_ids jsonb NOT NULL DEFAULT '[]'::jsonb;
CREATE INDEX IF NOT EXISTS idx_products_attribute_ids ON products USING gin (attribute_ids jsonb_path_ops);

-- 1) 历史空 key 回填：081→085 之间本表无任何写入路径，测试库里也没有数据。
--    仅对真实库里可能存在的空 key 行做一次确定性回填（attr-<id 前 8 位>）。
UPDATE product_attributes
SET key = 'attr-' || substr(replace(id::text, '-', ''), 1, 8)
WHERE key IS NULL OR btrim(key) = '';

-- 2) (project_id, key) 唯一：key 非空才生效（全空白 key 不占用唯一名额）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_product_attributes_project_key_nonempty
    ON product_attributes(project_id, key)
    WHERE btrim(key) <> '';

-- 3) 参与变体标记的显式约束（列已存在，仅补 CHECK；幂等用 DO 块）。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'product_attributes'::regclass
          AND conname = 'product_attributes_is_variation_check'
    ) THEN
        ALTER TABLE product_attributes
            ADD CONSTRAINT product_attributes_is_variation_check
            CHECK (is_variation IN (true, false));
    END IF;
END $$;

COMMENT ON COLUMN product_attributes.is_variation IS
    '是否参与变体生成（issue #7）：true=参与笛卡尔积（颜色/尺寸），false=仅展示（材质/产地）';
COMMENT ON COLUMN product_attributes.key IS
    '属性组稳定标识（工程内唯一，小写连字符；参与变体的组会被写入变体规格组合 option_values）';
COMMENT ON COLUMN products.attribute_ids IS
    '商品引用的属性组 id 数组（issue #7）：只存引用，属性组定义唯一一份，可被多个商品复用';
COMMENT ON COLUMN product_attributes.values IS
    '属性值数组 [{id,key,label,sort,enabled}]（issue #7）；历史纯字符串数组读取时兜底归一';
