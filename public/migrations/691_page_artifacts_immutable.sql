-- 691：产物身份按内容确定；同一草稿版本重建必须保留旧 Publication 引用的行。
-- 先建立内容唯一键，若历史数据冲突显式失败，不合并/删除任何已有产物身份。
CREATE UNIQUE INDEX IF NOT EXISTS uk_page_artifacts_page_hash
    ON page_artifacts (page_id, artifact_hash);
DROP INDEX IF EXISTS uk_page_artifacts_page_version_lang;
DROP INDEX IF EXISTS idx_page_artifacts_page_hash;
COMMENT ON COLUMN page_artifacts.lang IS '产物构建语言；同页同哈希归档幂等，不同内容各有不可变产物身份';
