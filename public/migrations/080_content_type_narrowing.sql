-- 080 · 内容类型收敛：contents 只保留 article，跨领域的类型枚举交给注册表
--
-- 背景（issue #4 / T2）：商品将成为独立领域模块（internal/module/product），不再寄居
-- 内容表；分类与品牌同样归商品域。因此内容实体类型收敛为 article。
--
-- 同时解掉两处「跨领域的类型枚举」：
--   · content_templates.entity_type —— 模板会承载商品详情等非内容领域，类型合法性由
--     运行期的实体类型注册表判定，不应在数据库层写死枚举；
--   · pages.pages_content_contract_check —— 去掉 product / category 分支：商品页由
--     PresentationInstance 自动发布，不走手工 Page。
--
-- 幂等：DROP CONSTRAINT IF EXISTS 后重建；重复执行安全。
-- 注册：public/migrations/register.go（Migration 080-content-type-narrowing）。

-- 1) contents：只允许 article（商品/分类改由领域模块自己的表承载）
ALTER TABLE contents DROP CONSTRAINT IF EXISTS contents_entity_type_check;
ALTER TABLE contents ADD CONSTRAINT contents_entity_type_check CHECK (entity_type = 'article');

-- 2) content_templates：解掉类型枚举（合法性由实体类型注册表判定）
ALTER TABLE content_templates DROP CONSTRAINT IF EXISTS content_templates_entity_type_check;

-- 3) pages：内容契约去掉 product / category 分支
ALTER TABLE pages DROP CONSTRAINT IF EXISTS pages_content_contract_check;
ALTER TABLE pages ADD CONSTRAINT pages_content_contract_check CHECK (
    (kind IN ('home', 'archive', 'search', 'notFound')
        AND content_target_type = 'none'
        AND content_target_id IS NULL)
    OR (kind = 'page'
        AND content_target_type = 'page'
        AND content_target_id IS NOT NULL)
    OR (kind = 'article'
        AND content_target_type = 'article'
        AND content_target_id IS NOT NULL)
    OR (kind = 'tag'
        AND content_target_type = 'tag'
        AND content_target_id IS NOT NULL)
);