-- 267 · publication_receipts 的 pending 部分索引（回执定时收敛的空转查询）
--
-- 背景：发布 / 改 URL / 回滚失败后会在 publication_receipts 留下 pending 回执，
-- 进程崩溃留下的那一条更是只可能靠收敛例程收掉。收敛例程按
-- receipt_state = 'pending' ORDER BY create_time 逐批领取（LIMIT + FOR UPDATE SKIP LOCKED），
-- 可观测接口还要随时数一次 pending 数量。
--
-- 这条查询恰恰在**空转**时最频繁：定时器每次触发都会跑一遍，而绝大多数时刻表里一条
-- pending 都没有。没有索引时它是全表扫描，而回执表只增不删（每次发布 / 改 URL / 回滚
-- 都登记一条），扫描成本随时间线性上涨 —— 成本正好长在「本不该有成本」的那条路径上。
--
-- 用**部分索引**（WHERE receipt_state = 'pending'）而不是整列索引：
--   · 索引里只留未结案的行，结案后索引项随之回收 —— 体积恒等于「当前待办数」，
--     与表的历史规模无关；整列索引则会随每次发布永久增长。
--   · 领取查询与 pending 计数都带 receipt_state = 'pending' 等值条件，部分谓词可被证明蕴含，
--     于是两者都能走这条索引；ORDER BY create_time 直接吃索引序，领取不需要额外排序。
--
-- 键列只有 create_time（领取的排序键）：source_type / action 是领到手之后再过滤的，
-- 放进键列既撑大索引又用不上 —— 待办数本该很小，过滤代价可以忽略。
--
-- 幂等：CREATE INDEX IF NOT EXISTS；注册侧 CheckSQL 按索引名判断是否存在
-- （限定 current_schema()，否则并发测试或残留 schema 里的同名索引会让判定恒为真而静默跳过）。
CREATE INDEX IF NOT EXISTS idx_publication_receipts_pending
    ON publication_receipts (create_time)
    WHERE receipt_state = 'pending';
