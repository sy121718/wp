-- 313_article_detail_template_props.sql — 文章详情模板的非法控件值修正（repair）。
--
-- 159 种下的默认文章详情模板声明了两处**不在控件白名单里**的值：
--   · core.text 的内容模式写成 plain / rich —— 白名单只有 richtext / plaintext；
--   · core.image 的宽高比写成 ratio   —— 字段名是 aspectRatio。
-- 后果不是「样式不对」，而是模板文档过不了 ValidatePageTolerant：
-- 每一次 POST /api/presentation/create（entityType=article）都以
-- 「Blueprint 文档格式非法」失败 —— **文章详情页一页都建不出来**，
-- 而商品侧（085）文档合法，于是表现成「商品详情能建、文章详情不能建」。
--
-- 159 是**种子**（registerSeed），按仓库约定种子要随内容一起改（已同步修好），
-- 但它的判定是「已有 article 模板就跳过」—— 已建库不会重跑，故这里补一次幂等修复。
--
-- 安全边界：只重写**与 159 原样一致**的模板（jsonb 相等比较，与文本格式无关）——
-- 作者改过的模板一个字节都不动（作者有意保留旧字段是他的选择）。
-- 注册：public/migrations/register_analytics.go。

DO $$
DECLARE
    v_old jsonb := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"art-section","type":"core.container","props":{"tag":"article","layout":{"engine":"flex","flex":{"direction":"column","gap":"1rem"}}},"children":[{"id":"art-title","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h1"}},{"id":"art-featured","type":"core.image","props":{"binding":{"field":"article.featuredImage"},"ratio":"16:9"}},{"id":"art-excerpt","type":"core.text","props":{"mode":"plain","binding":{"field":"article.excerpt"},"plainTag":"p"}},{"id":"art-body","type":"core.text","props":{"mode":"rich","binding":{"field":"article.body"}}}]}]}'::jsonb;
    v_new jsonb := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"art-section","type":"core.container","props":{"tag":"article","layout":{"engine":"flex","flex":{"direction":"column","gap":"1rem"}}},"children":[{"id":"art-title","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h1"}},{"id":"art-featured","type":"core.image","props":{"binding":{"field":"article.featuredImage"},"aspectRatio":"16:9"}},{"id":"art-excerpt","type":"core.text","props":{"mode":"plaintext","binding":{"field":"article.excerpt"},"plainTag":"p"}},{"id":"art-body","type":"core.text","props":{"mode":"richtext","binding":{"field":"article.body"}}}]}]}'::jsonb;
BEGIN
    UPDATE content_template_versions v
    SET document = v_new,
        source_hash = encode(sha256(v_new::text::bytea), 'hex')
    FROM content_templates t
    WHERE v.id = t.current_version_id
      AND t.entity_type = 'article'
      AND v.document = v_old;

    UPDATE content_templates
    SET draft_document = v_new, update_time = now()
    WHERE entity_type = 'article' AND draft_document = v_old;

    -- 顺带修商品详情模板的货币符号：085 / 087b 种下的是 "¥"（模板作者的写法），
    -- 而这个站是澳洲站。用 jsonb_set 按**路径**改，不做整份相等匹配 ——
    -- 这样无论模板被 087b 补过槽位没有，都只动 currency 这一个字段。
    UPDATE content_templates
    SET draft_document = jsonb_set(draft_document, '{root,0,children,0,props,currency}', '"$"'),
        update_time = now()
    WHERE entity_type = 'product'
      AND draft_document #>> '{root,0,children,0,props,currency}' = '¥';

    UPDATE content_template_versions v
    SET document = jsonb_set(v.document, '{root,0,children,0,props,currency}', '"$"'),
        source_hash = encode(sha256(jsonb_set(v.document, '{root,0,children,0,props,currency}', '"$"')::text::bytea), 'hex')
    FROM content_templates t
    WHERE v.id = t.current_version_id
      AND t.entity_type = 'product'
      AND v.document #>> '{root,0,children,0,props,currency}' = '¥';
END $$;
