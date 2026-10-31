-- 701 · 手工页面激活账本的成功 attempt 身份（复用预登记回执 id）。
--
-- 背景：一次发布在切访问面**之前**就预登记一条 publication_receipts（pending），
-- 回执 id 是这次尝试的唯一身份。激活成功时，必须把「是哪一次尝试成功」写进账本 ——
-- 否则收敛例程只能靠 (version, artifact) 判断，而**同一代输入、同一份产物被判两次
-- 成功**（主链一次、崩溃后补完成一次）在账本上完全不可区分：后者会把前者的结果
-- 当自己的，恢复也就失去了「这条回执是否已经兑现」的事实依据。
--
-- 只加一列，不回填：
--   · page_publications.receipt_id bigint NULL —— 成功激活它的那条回执 id；
--   · NULL = 历史行（本迁移之前成功、或调用链路没有回执设施），
--     收敛按既有的「链接身份 + 账本归属」判据降级，**不当成任何回执的成功尝试**。
--
-- 为什么不加外键：publication_receipts 归 publication 模块所有，且保留期清理会删旧回执
--（页面账本比回执长寿）；外键会让清理被账本挡住，或让历史行悬空。跨聚合引用只存 id 事实。
-- 不加索引：唯一读法是「按 (page_id, lang) 取一行再看这一列」，主键已覆盖。
--
-- 幂等：ADD COLUMN IF NOT EXISTS。可重复执行。
-- 不加 BEGIN/COMMIT：迁移器（ApplyStatements）已把整批语句包在同一个事务里。

ALTER TABLE page_publications ADD COLUMN IF NOT EXISTS receipt_id bigint;

COMMENT ON COLUMN page_publications.receipt_id IS '成功激活该语言产物的发布回执 id（复用预登记回执 ID 作为 attempt 身份）；NULL = 历史行，收敛退既有判据';
