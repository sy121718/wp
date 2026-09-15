-- 159 · 默认文章详情内容模板（EDT-002）。
--
-- 商品有 085 种子，文章此前缺失导致发布被 articleNoTemplateHint 拦截。
-- 布局：container → heading(title) + image(featuredImage) + text(excerpt) + text(body rich)。
--
-- 幂等：已有 article 模板则跳过；无工程时跳过（下次启动重试）。

DO $$
DECLARE
    v_project uuid;
    v_tpl     uuid := gen_random_uuid();
    v_ver     uuid := gen_random_uuid();
    v_doc     jsonb := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"art-section","type":"core.container","props":{"tag":"article","layout":{"engine":"flex","flex":{"direction":"column","gap":"1rem"}}},"children":[{"id":"art-title","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h1"}},{"id":"art-featured","type":"core.image","props":{"binding":{"field":"article.featuredImage"},"ratio":"16:9"}},{"id":"art-excerpt","type":"core.text","props":{"mode":"plain","binding":{"field":"article.excerpt"},"plainTag":"p"}},{"id":"art-body","type":"core.text","props":{"mode":"rich","binding":{"field":"article.body"}}}]}]}'::jsonb;
BEGIN
    IF EXISTS (SELECT 1 FROM content_templates WHERE entity_type = 'article') THEN
        RETURN;
    END IF;
    SELECT id INTO v_project FROM projects ORDER BY create_time LIMIT 1;
    IF v_project IS NULL THEN
        RETURN;
    END IF;
    INSERT INTO content_templates (id, project_id, name, entity_type, draft_document, draft_version, current_version_id, create_time, update_time)
    VALUES (v_tpl, v_project, '文章详情页', 'article', v_doc, 1, v_ver, now(), now());
    INSERT INTO content_template_versions (id, template_id, version, document, source_hash, created_by, create_time)
    VALUES (v_ver, v_tpl, 1, v_doc, encode(sha256(v_doc::text::bytea), 'hex'), '00000000-0000-0000-0000-000000000000'::uuid, now());
END $$;
