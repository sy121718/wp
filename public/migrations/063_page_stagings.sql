-- 063 · page_stagings：页面「每语言暂存产物」（多语言 P3）
--
-- 与 062 同因：pages.staged_artifact_id 单值，Build(en-US) 会覆盖 Build(zh-CN) 的暂存指针，
-- 随后 Publish(zh-CN) 复构建出的 hash 与「暂存指针」不一致 → ErrRebuildRequired，
-- 「先构建两种语言、再逐个发布」的流程走不通。
-- 修法：暂存指针按 (page_id, lang) 独立落表；pages.staged_artifact_id 保留为
-- 「最近构建语言」的单值镜像（页面列表/详情投影兼容）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS；新表用默认「表存在即跳过」检查。

CREATE TABLE IF NOT EXISTS page_stagings (
    page_id       uuid        NOT NULL,
    lang          text        NOT NULL,
    artifact_id   uuid        NOT NULL,
    artifact_hash text        NOT NULL DEFAULT '',
    draft_version bigint      NOT NULL DEFAULT 0,
    updated_at    timestamptz NOT NULL,
    PRIMARY KEY (page_id, lang)
);

COMMENT ON TABLE page_stagings IS '页面每语言暂存产物（多语言 P3）：主键 (page_id, lang)';
