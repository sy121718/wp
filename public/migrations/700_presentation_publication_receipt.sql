-- 700 · 自动发布实例激活账本的成功 attempt 身份（复用预登记回执 id）。
--
-- 背景：一次多语言发布在切访问面**之前**就为每种语言预登记一条 publication_receipts
--（pending），回执 id 是这次尝试的唯一、持久的身份。激活成功时，必须把「是哪一次尝试
-- 成功」写进账本，否则收敛例程只能靠 artifact_id 判断归属 —— 而内容寻址 + 同版本重建
-- 会**复用同一条产物行**（GetArtifactByHashTx），两个不同 attempt 的 artifact_id 完全相同，
--「这条账本是不是我写的」在库上不可区分。判错的代价是双向的：把别人的成功当自己的
--（重复补齐，掩盖真实归属），或把自己的成功当别人的（误判 superseded，触发无谓重建）。
--
-- 只加一列，不回填：
--   · presentation_publications.receipt_id bigint NULL —— 成功激活该语言的回执 id；
--   · NULL = 本迁移之前成功的历史行，读侧一律按「未知」退回 artifact_id 比较，
--     **不当成任何回执的成功尝试**。
--
-- 为什么不加外键：publication_receipts 归 publication 模块所有，且保留期清理会删旧回执
--（实例账本比回执长寿）；外键会让清理被账本挡住，或让历史行悬空。跨聚合引用只存 id 事实。
-- 不加索引：唯一读法是「按 (presentation_id, lang) 取一行再看这一列」，主键已覆盖。
--
-- 与 701（page_publications.receipt_id，page owner）是同一次协议的两侧，各改各的表。
-- 幂等：ADD COLUMN IF NOT EXISTS。可重复执行。
-- 不加 BEGIN/COMMIT：迁移器（ApplyStatements）已把整批语句包在同一个事务里。

ALTER TABLE presentation_publications ADD COLUMN IF NOT EXISTS receipt_id bigint;

COMMENT ON COLUMN presentation_publications.receipt_id IS '成功激活该语言产物的发布回执 id（复用预登记回执 ID 作为 attempt 身份）；NULL = 历史行，收敛退 artifact_id 判据';
