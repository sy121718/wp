-- 209 · blocks.id 回到 uuid（主键选型判据：对外边界用不可枚举标识）。
--
-- 背景：201（DB-019/020 第一批）按「主键统一 bigint」把 blocks 划到了自增侧。
-- 复核判据后确认那条路走错了方向 —— 判据是「这个 id 会不会出现在系统边界之外」，
-- 而 blocks 两条都踩：
--   ① 对外有 /api/block/*（列表 / 详情 / 克隆）；
--   ② props.blockId 写进 Page Document，经 jsonb_path_query_array(draft_document,'$.**.blockId')
--      参与引用判定，上面还挂着 GIN 部分索引 idx_pages_blockref。
-- 同一批里的 build_jobs / page_site_slots / publication_receipts / inventory_change_reasons
-- 是内部流水与字典，继续用 bigint，本迁移不动它们。
--
-- 本迁移做四件事，顺序有意义：
--   1. 建旧→新映射并持久保留（审计痕迹：哪些块换了哪个 id）；
--   2. 换 blocks.id 类型。零入度：没有任何外键引用 blocks.id，只需重建主键，
--      并恢复 021 建表时的 DEFAULT gen_random_uuid()（201 换类型时丢掉了它）；
--   3. 按映射重写全部文档里的块引用。三种键形态：
--      props.blockId（root 树任意深度）、settings.structure.headerBlockId / footerBlockId、
--      settings.slots.*（键是槽位名、值是块 id，无法按键名识别，单独分支处理）；
--   4. 重写 page_dependencies 里 global_component 的 dependency_key。
--
-- 覆盖的文档列（含历史快照 —— 漏掉快照的表现是「回滚到历史版本后，页面引用的块不存在」）：
--   blocks.document、pages.draft_document、page_revisions.draft_document、
--   page_artifacts.source_document、content_templates.draft_document、
--   content_template_versions.document、blueprints.draft_document、
--   blueprint_versions.document、document_snapshots.document、themes.settings。
--
-- 为什么用一个 IMMUTABLE 递归函数而不是逐路径替换：blockId 可以嵌在 root 树的
-- 任意容器层级，路径无法枚举；函数对整个文档是恒等的（不含目标键的原样返回），
-- 因此对无关行无副作用，可以整表 UPDATE 而不必先扫一遍 LIKE。
--
-- 幂等：判定按「blocks.id 已是 uuid」。映射表存在即复用，重放不会重新分配 id。

-- ========== 1. 映射表（持久保留） ==========
CREATE TABLE IF NOT EXISTS block_id_uuid_map (
    old_id    bigint PRIMARY KEY,
    new_id    uuid NOT NULL UNIQUE,
    mapped_at timestamptz NOT NULL DEFAULT now()
);

-- ========== 2. 生成映射（只对尚未映射的 bigint id） ==========
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'blocks'
                 AND column_name = 'id' AND data_type = 'bigint') THEN
        INSERT INTO block_id_uuid_map (old_id, new_id)
        SELECT b.id, gen_random_uuid()
          FROM blocks b
         WHERE NOT EXISTS (SELECT 1 FROM block_id_uuid_map m WHERE m.old_id = b.id);
    END IF;
END $$;

-- ========== 3. 换 blocks.id 类型 ==========
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'blocks'
                 AND column_name = 'id' AND data_type = 'bigint') THEN
        ALTER TABLE blocks ADD COLUMN IF NOT EXISTS id_uuid uuid;
        UPDATE blocks b SET id_uuid = m.new_id
          FROM block_id_uuid_map m WHERE m.old_id = b.id;
        -- 表可能为空或映射缺失：兜底给未映射的行分配新 uuid，再置 NOT NULL
        UPDATE blocks SET id_uuid = gen_random_uuid() WHERE id_uuid IS NULL;
        ALTER TABLE blocks ALTER COLUMN id_uuid SET NOT NULL;
        ALTER TABLE blocks ALTER COLUMN id_uuid SET DEFAULT gen_random_uuid();
        ALTER TABLE blocks DROP CONSTRAINT IF EXISTS blocks_pkey;
        ALTER TABLE blocks DROP COLUMN id;
        ALTER TABLE blocks RENAME COLUMN id_uuid TO id;
        ALTER TABLE blocks ADD CONSTRAINT blocks_pkey PRIMARY KEY (id);
    END IF;
END $$;

-- ========== 4. 递归替换函数（临时，末尾删除） ==========
CREATE OR REPLACE FUNCTION fn_rewrite_block_ids(doc jsonb, mapping jsonb)
RETURNS jsonb
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    k        text;
    v        jsonb;
    out_obj  jsonb := '{}'::jsonb;
    out_arr  jsonb := '[]'::jsonb;
    slot_obj jsonb := '{}'::jsonb;
BEGIN
    IF doc IS NULL OR mapping IS NULL THEN
        RETURN doc;
    END IF;

    IF jsonb_typeof(doc) = 'object' THEN
        FOR k, v IN SELECT e.key, e.value FROM jsonb_each(doc) AS e LOOP
            IF k = 'slots' AND jsonb_typeof(v) = 'object' THEN
                -- settings.slots：{槽位名: 块 id}。键名是任意槽位，不可能是 blockId 这类固定名，
                -- 所以只能对「值」做映射；未命中的值原样保留（可能是将来的非块槽位）。
                SELECT COALESCE(jsonb_object_agg(s.key,
                           CASE WHEN mapping ? s.value
                                THEN to_jsonb(mapping ->> s.value)
                                ELSE to_jsonb(s.value) END), '{}'::jsonb)
                  INTO slot_obj
                  FROM jsonb_each_text(v) AS s;
                out_obj := out_obj || jsonb_build_object(k, slot_obj);
            ELSIF k IN ('blockId', 'headerBlockId', 'footerBlockId')
                  AND jsonb_typeof(v) = 'string'
                  AND mapping ? (v #>> '{}') THEN
                out_obj := out_obj || jsonb_build_object(k, to_jsonb(mapping ->> (v #>> '{}')));
            ELSE
                out_obj := out_obj || jsonb_build_object(k, fn_rewrite_block_ids(v, mapping));
            END IF;
        END LOOP;
        RETURN out_obj;
    ELSIF jsonb_typeof(doc) = 'array' THEN
        FOR v IN SELECT e.value FROM jsonb_array_elements(doc) AS e LOOP
            out_arr := out_arr || jsonb_build_array(fn_rewrite_block_ids(v, mapping));
        END LOOP;
        RETURN out_arr;
    END IF;

    RETURN doc;
END;
$$;

-- ========== 5. 重写文档（每列一条，整表 UPDATE；函数对无关行恒等） ==========
DO $$
DECLARE
    m jsonb;
BEGIN
    SELECT jsonb_object_agg(old_id::text, new_id::text) INTO m FROM block_id_uuid_map;
    IF m IS NULL THEN
        RETURN;  -- 没有映射（空库）：文档里不可能有可见的块引用
    END IF;

    UPDATE blocks SET document = fn_rewrite_block_ids(document, m) WHERE document IS NOT NULL;
    UPDATE pages SET draft_document = fn_rewrite_block_ids(draft_document, m) WHERE draft_document IS NOT NULL;
    UPDATE page_revisions SET draft_document = fn_rewrite_block_ids(draft_document, m) WHERE draft_document IS NOT NULL;
    UPDATE page_artifacts SET source_document = fn_rewrite_block_ids(source_document, m) WHERE source_document IS NOT NULL;
    UPDATE content_templates SET draft_document = fn_rewrite_block_ids(draft_document, m) WHERE draft_document IS NOT NULL;
    UPDATE content_template_versions SET document = fn_rewrite_block_ids(document, m) WHERE document IS NOT NULL;
    UPDATE blueprints SET draft_document = fn_rewrite_block_ids(draft_document, m) WHERE draft_document IS NOT NULL;
    UPDATE blueprint_versions SET document = fn_rewrite_block_ids(document, m) WHERE document IS NOT NULL;
    UPDATE document_snapshots SET document = fn_rewrite_block_ids(document, m) WHERE document IS NOT NULL;
    UPDATE themes SET settings = fn_rewrite_block_ids(settings, m) WHERE settings IS NOT NULL;

    -- 依赖索引：block 引用登记为 global_component，key 就是块 id
    UPDATE page_dependencies d SET dependency_key = m2.new_id::text
      FROM block_id_uuid_map m2
     WHERE d.dependency_kind = 'global_component' AND d.dependency_key = m2.old_id::text;
END $$;

-- ========== 6. 清理临时函数（映射表保留为审计痕迹） ==========
DROP FUNCTION IF EXISTS fn_rewrite_block_ids(jsonb, jsonb);
