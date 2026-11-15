-- table: sys_translation（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.sys_translation (
    source_hash text NOT NULL,
    context text NOT NULL,
    lang text NOT NULL,
    source_text text NOT NULL,
    target_text text NOT NULL,
    engine text DEFAULT 'manual'::text NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT sys_translation_updated_at_not_null NOT NULL,
    project_id uuid,
    CONSTRAINT ck_sys_translation_engine CHECK ((engine = ANY (ARRAY['manual'::text, 'ai'::text, 'po'::text]))),
    CONSTRAINT ck_sys_translation_hash CHECK ((source_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT ck_sys_translation_target CHECK ((target_text <> ''::text))
);

ALTER TABLE ONLY public.sys_translation FORCE ROW LEVEL SECURITY;

CREATE UNIQUE INDEX uq_sys_translation_scope_key ON public.sys_translation USING btree (project_id, source_hash, context, lang) NULLS NOT DISTINCT;

CREATE POLICY project_isolation ON public.sys_translation USING (((project_id IS NULL) OR (project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid))) WITH CHECK (((project_id IS NULL) OR (project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)));

ALTER TABLE public.sys_translation ENABLE ROW LEVEL SECURITY;

-- @phase idx
CREATE INDEX idx_sys_translation_context_lang ON public.sys_translation USING btree (context, lang);

CREATE INDEX idx_sys_translation_engine_lang ON public.sys_translation USING btree (engine, lang);

CREATE INDEX idx_sys_translation_hash_lang ON public.sys_translation USING btree (source_hash, lang);

CREATE INDEX idx_sys_translation_project_hash_lang ON public.sys_translation USING btree (project_id, source_hash, lang);

-- @phase fk
ALTER TABLE ONLY public.sys_translation
    ADD CONSTRAINT sys_translation_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.sys_translation IS '内容寻址翻译表（构建器内联文本 + CMS 字段共用，多语言 P5a）';

COMMENT ON COLUMN public.sys_translation.source_hash IS 'sha256hex(source_text) 小写十六进制 64 字符（只对原文做 hash，不含 context/lang）';

COMMENT ON COLUMN public.sys_translation.context IS '语境：组件类型.字段名 / 实体.字段名，如 core.button.text、product.name';

COMMENT ON COLUMN public.sys_translation.lang IS '目标语言，如 en-US';

COMMENT ON COLUMN public.sys_translation.source_text IS '冗余原文：供工作台对照与 hash 校验';

COMMENT ON COLUMN public.sys_translation.target_text IS '译文（非空；无译文时构建期回退原文，不写空串）';

COMMENT ON COLUMN public.sys_translation.engine IS '译文来源 manual | ai | po（仅筛选审阅，不参与取值逻辑）';

COMMENT ON COLUMN public.sys_translation.update_time IS '最后写入时间';

COMMENT ON COLUMN public.sys_translation.project_id IS '工程作用域：NULL = 全局共享（既有行语义，跨站点复用）；非 NULL = 仅该工程使用，构建期优先于全局行';

COMMENT ON INDEX public.idx_sys_translation_project_hash_lang IS '构建期按工程取内容译文：project_id + source_hash + lang（审计 I18N-009）';

COMMENT ON INDEX public.uq_sys_translation_scope_key IS '作用域唯一键（取代 066 的主键，审计 I18N-009）：NULLS NOT DISTINCT 让全局行（project_id IS NULL）同样唯一，ON CONFLICT 可直接推断';
