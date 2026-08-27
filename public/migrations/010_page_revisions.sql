-- 010_page_revisions.sql
-- 为已存在的 pages 表增加不可变草稿修订记录。
CREATE TABLE IF NOT EXISTS page_revisions (
    id              uuid PRIMARY KEY,
    page_id         uuid NOT NULL REFERENCES pages(id),
    version         bigint NOT NULL,
    draft_path      text NOT NULL,
    draft_document  jsonb NOT NULL,
    source_hash     text NOT NULL,
    created_at      timestamptz NOT NULL,
    UNIQUE (page_id, version)
);