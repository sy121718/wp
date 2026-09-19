-- 282 · presentation_instances 渲染模式（render_mode）——商品页双轨。
--
-- 背景（docs/04-C-instance-override.md，双轨定稿）：
--   商品详情页有两种承载形态，由本列显式区分，不再靠「override_document 是否为空」推断：
--     template：文档 = 绑定模板的文档（每次构建参与；模板更新可全局下发）——默认值，既有行为零回归；
--     document：文档 = override_document（该商品自己的文档；模板更新不影响它）。
--
-- 为什么必须显式成列而不是靠 override_document 推断：
--   · 「改了又改回去（与模板一致）」「重新套用预设后」这两种状态用空/非空推断会漂移；
--   · 模板更新的 stale 传播要按模式分流（只重建 template 模式的实例），
--     SQL 里需要一列可判定条件，而不是一次 JSON 比较。
--
-- 回填：281 期间「override_document 非空」即隐式 document 模式，本迁移把它显式化。
-- 幂等：ADD COLUMN IF NOT EXISTS + 约束按 pg_constraint 存在判断（限定 regclass 走 search_path）。
ALTER TABLE presentation_instances ADD COLUMN IF NOT EXISTS render_mode text NOT NULL DEFAULT 'template';

-- 回填隐式语义（重复执行无副作用：条件已满足的行不再命中）。
UPDATE presentation_instances SET render_mode = 'document'
 WHERE override_document IS NOT NULL AND render_mode = 'template';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'ck_presentation_instances_render_mode'
           AND conrelid = 'presentation_instances'::regclass
    ) THEN
        ALTER TABLE presentation_instances
            ADD CONSTRAINT ck_presentation_instances_render_mode
            CHECK (render_mode IN ('template', 'document'));
    END IF;
END $$;

COMMENT ON COLUMN presentation_instances.render_mode IS '渲染模式 template=跟随绑定模板 | document=该商品独立文档（override_document）';
