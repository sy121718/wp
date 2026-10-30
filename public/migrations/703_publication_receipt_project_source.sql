-- 703 · 回执归属新增「项目级」来源（publication_receipts.source_type 放宽）。
--
-- 背景：重定向（redirect）链接的归属是「工程 + 路径」，行上的 page_id / presentation_id
-- 都是 NULL。删掉这样一条链接必须撤掉它的符号链接，也就必须有一条下线回执 ——
-- 否则链撤了、回执没留痕，崩溃后收敛例程无从判断这条链该不该被补回来。
-- 但回执归属是 (source_type, source_id)：source_id 是 uuid 列、恢复侧要据此取实体级锁，
-- 而 redirect 行没有任何真实实体 id。伪造一个 page id 会让恢复例程领到「不属于它的意图」
-- 并按错误归属撤链 —— 所以本迁移给 source_type 增加第三个值 'project'，
-- SourceID 就是真实工程 UUID（见 internal/module/publication/dto/publication_resp.go 的
-- ReceiptSourceProject 与其使用约束）。
--
-- 只放宽 CHECK，不改列型、不回填、不加索引、不加默认值：
--   · source_id 仍是 uuid —— 'project' 的取值同样是 uuid（工程 id），列型不需要动；
--   · page / presentation 两个既有值逐字保留 —— 本迁移只做加法；
--   · 不加其它来源（不放开成任意文本）：收敛例程按 source_type 筛选「自己该管的那一批」，
--     允许任意来源会让它把别人的回执当成自己的。
--
-- 约束名 publication_receipts_source_type_check 来自 init_builder_schema.sql 的内联 CHECK
-- 被 PG 自动命名的结果（同表 receipt_state 对应 publication_receipts_receipt_state_check），
-- 与 699 放宽 receipt_state 是同一张表上的同型写法。
--
-- 幂等：约束先 DROP IF EXISTS 再 ADD，可重复执行。
-- 不加 BEGIN/COMMIT：迁移器（ApplyStatements）已把整批语句包在同一个事务里，
-- 文件内再开事务会与外层事务冲突。

ALTER TABLE publication_receipts DROP CONSTRAINT IF EXISTS publication_receipts_source_type_check;
ALTER TABLE publication_receipts ADD CONSTRAINT publication_receipts_source_type_check
    CHECK (source_type IN ('page', 'presentation', 'project'));

-- 只写来源语义，不写 artifact 列的形状：本迁移**没有**给 from/to artifact 加任何约束，
-- 在这里声称「均为空」等于用注释虚构一条不存在的 DB 规则（Begin 目前只对 To 有可空例外）。
-- artifact 侧要不要强制空，是 publication 协议的事，由该域 owner 定，不夹在这条迁移里。
COMMENT ON COLUMN publication_receipts.source_type IS '回执归属类型：page / presentation 为实体级，project 为工程级（仅 action=deactivate，source_id 是真实工程 UUID）';
