-- 工程级默认内容模板：旧 seed 085（商品详情）与 159（文章详情）的等价物。
-- 按工程补齐：每个工程各一份 product / article 默认模板；已有同类型模板则跳过。
-- 这是启动期系统数据的一部分，不属于业务数据；新增工程后下一次 schema.Apply 会补齐。
DO $$
DECLARE
    p record;
    v_tpl uuid;
    v_ver uuid;
    v_doc jsonb;
BEGIN
    FOR p IN SELECT id FROM public.projects LOOP
        IF NOT EXISTS (SELECT 1 FROM public.content_templates
                       WHERE project_id = p.id AND entity_type = 'product') THEN
            v_tpl := gen_random_uuid();
            v_ver := gen_random_uuid();
            v_doc := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd-section","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"pd-body","type":"core.product","props":{"source":"product","mediaField":"product.defaultImage","galleryField":"product.images","titleField":"product.name","subtitleField":"product.subtitle","priceField":"product.priceRange","comparePriceField":"product.comparePrice","descriptionField":"product.description","optionsField":"product.options","variantsField":"product.variants","currency":"¥","titleTag":"h1"}}]}]}'::jsonb;
            INSERT INTO public.content_templates
                (id, project_id, name, entity_type, draft_document, draft_version, current_version_id,
                 create_time, update_time, is_default, template_role)
            VALUES (v_tpl, p.id, '商品详情页', 'product', v_doc, 1, v_ver, now(), now(), false, 'detail');
            INSERT INTO public.content_template_versions
                (id, template_id, version, document, source_hash, created_by, create_time)
            VALUES (v_ver, v_tpl, 1, v_doc, encode(sha256(v_doc::text::bytea), 'hex'),
                    '00000000-0000-0000-0000-000000000000'::uuid, now());
        END IF;

        IF NOT EXISTS (SELECT 1 FROM public.content_templates
                       WHERE project_id = p.id AND entity_type = 'article') THEN
            v_tpl := gen_random_uuid();
            v_ver := gen_random_uuid();
            v_doc := '{"settings":{"layout":{"mode":"full"}},"root":[{"id":"art-section","type":"core.container","props":{"tag":"article","layout":{"engine":"flex","flex":{"direction":"column","gap":"1rem"}}},"children":[{"id":"art-title","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h1"}},{"id":"art-featured","type":"core.image","props":{"binding":{"field":"article.featuredImage"},"aspectRatio":"16:9"}},{"id":"art-excerpt","type":"core.text","props":{"mode":"plaintext","binding":{"field":"article.excerpt"},"plainTag":"p"}},{"id":"art-body","type":"core.text","props":{"mode":"richtext","binding":{"field":"article.body"}}}]}]}'::jsonb;
            INSERT INTO public.content_templates
                (id, project_id, name, entity_type, draft_document, draft_version, current_version_id,
                 create_time, update_time, is_default, template_role)
            VALUES (v_tpl, p.id, '文章详情页', 'article', v_doc, 1, v_ver, now(), now(), false, 'detail');
            INSERT INTO public.content_template_versions
                (id, template_id, version, document, source_hash, created_by, create_time)
            VALUES (v_ver, v_tpl, 1, v_doc, encode(sha256(v_doc::text::bytea), 'hex'),
                    '00000000-0000-0000-0000-000000000000'::uuid, now());
        END IF;
    END LOOP;
END $$;
