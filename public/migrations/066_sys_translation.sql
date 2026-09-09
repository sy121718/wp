-- 066 · sys_translation：内容寻址翻译表（多语言 P5a，docs/06-D-site-i18n.md §7.3）
--
-- 定位（决策 F2/F3/F4/F17）：翻译是「原文之外的附加层」，按 (source_hash, context, lang)
-- 内容寻址；与 sys_i18n 分工——sys_i18n 跟代码发布走（开发者 key），本表跟内容编辑走
-- （构建器内联文本 + CMS 字段，共用同一张表，靠 context 前缀区分）。
-- 改原文 → source_hash 变 → 旧译文自动失效（防线 1），老数据零迁移。
--
-- 无 status 列：刻意不做 draft/confirmed 状态机（决策 F8，有译文就用，无译文回退原文）。
-- 无 project_id：跨页面/跨站点复用是全局的（决策 F11/D12）。
-- engine 只用于工作台筛选审阅，不参与取值逻辑（决策 F4/F9）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，重复执行安全；
-- register.go 侧按「表存在」判定（默认 CheckSQL）跳过重跑。

CREATE TABLE IF NOT EXISTS sys_translation (
    source_hash  text        NOT NULL,
    context      text        NOT NULL,
    lang         text        NOT NULL,
    source_text  text        NOT NULL,
    target_text  text        NOT NULL,
    engine       text        NOT NULL DEFAULT 'manual',
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_hash, context, lang),
    CONSTRAINT ck_sys_translation_hash   CHECK (source_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_sys_translation_engine CHECK (engine IN ('manual', 'ai', 'po')),
    CONSTRAINT ck_sys_translation_target CHECK (target_text <> '')
);

-- 构建期批量取数走 (source_hash, lang)（§7.7 唯一查询形态）。
CREATE INDEX IF NOT EXISTS idx_sys_translation_hash_lang
    ON sys_translation (source_hash, lang);

-- 工作台按页面/组件浏览。
CREATE INDEX IF NOT EXISTS idx_sys_translation_context_lang
    ON sys_translation (context, lang);

-- 工作台「只看 AI 翻译的」筛选。
CREATE INDEX IF NOT EXISTS idx_sys_translation_engine_lang
    ON sys_translation (engine, lang);

COMMENT ON TABLE sys_translation IS '内容寻址翻译表（构建器内联文本 + CMS 字段共用，多语言 P5a）';
COMMENT ON COLUMN sys_translation.source_hash IS 'sha256hex(source_text) 小写十六进制 64 字符（只对原文做 hash，不含 context/lang）';
COMMENT ON COLUMN sys_translation.context IS '语境：组件类型.字段名 / 实体.字段名，如 core.button.text、product.name';
COMMENT ON COLUMN sys_translation.lang IS '目标语言，如 en-US';
COMMENT ON COLUMN sys_translation.source_text IS '冗余原文：供工作台对照与 hash 校验';
COMMENT ON COLUMN sys_translation.target_text IS '译文（非空；无译文时构建期回退原文，不写空串）';
COMMENT ON COLUMN sys_translation.engine IS '译文来源 manual | ai | po（仅筛选审阅，不参与取值逻辑）';
COMMENT ON COLUMN sys_translation.updated_at IS '最后写入时间';
