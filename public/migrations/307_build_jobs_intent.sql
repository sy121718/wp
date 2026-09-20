-- 307 · build_jobs 待办去重键按「构建输入 + 语言 + 意图」分开（审计 ARCH-04）。
--
-- 背景：第 21 个页面之后的依赖重建入队时，任务只带来源 id —— 消费侧既不知道要构建
-- 哪几种语言，也不知道「此前哪些语言已发布、构建后要不要回写线上」，同步路径与异步
-- 路径因此语义不同（报告 ARCH-04）。修法是把语言与意图显式入库（295 已建列），而队列
-- 的待办去重键必须跟着这两个维度走，否则：
--   ① 键里没有 lang：同一页面同一输入、不同语言的两条待办会撞唯一键被**误去重** ——
--      多语言站点只有一种语言被构建，其余语言停在旧字节，且没有任何报错；
--   ② 键里没有 intent：人工发起的手工构建会被同键的依赖重建任务吞掉 ——
--      后台点「构建」看起来排上了，实际被去重成一条依赖重建。
--
-- 不改来源互斥索引 uq_build_jobs_running_source (source_type, source_id) WHERE status='running'：
-- 同步路径本来就是「同一页逐语言构建 / 发布」，第一版不引入语言间并发这个新面
-- （判定与理由见 internal/module/build/model/build_model.go 的 claimSQL 注释）。

-- 存量归并（幂等、可重复执行）：新键是旧键的**超集**，所以只要 171 的唯一索引在位，
-- 存量 pending 行不可能在新键下冲突 —— 这段是给「只跑了部分迁移、或有人手工删过索引」
-- 的库兜底。归并规则：同来源、同输入、同语言、同意图的重复待办只保留 id 最小
--（最先入队、最该被认领）的那条，其余标 superseded 并写明原因。
-- 不丢工作：被合并的那份工作与保留下来的那条完全同键，可由存活那条原样重做。
UPDATE build_jobs b
   SET status = 'superseded',
       completed_at = now(),
       error_message = COALESCE(b.error_message, '同来源同输入同语言同意图的重复待办（迁移 307 归并）：保留最先入队的一条，本行合并为 superseded')
 WHERE b.status = 'pending'
   AND EXISTS (
       SELECT 1 FROM build_jobs k
        WHERE k.status = 'pending'
          AND k.source_type = b.source_type
          AND k.source_id = b.source_id
          AND k.build_input_hash = b.build_input_hash
          AND k.lang = b.lang
          AND k.intent = b.intent
          AND k.id < b.id
   );

-- 部分唯一索引先 DROP 再 CREATE（本迁移可重复执行）。
DROP INDEX IF EXISTS uq_build_jobs_pending;
CREATE UNIQUE INDEX uq_build_jobs_pending
    ON build_jobs (source_type, source_id, build_input_hash, lang, intent)
 WHERE status = 'pending';

-- 来源互斥索引按原样重新声明（IF NOT EXISTS，只为把「本迁移之后它必须仍在位」写进文件）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_build_jobs_running_source
    ON build_jobs (source_type, source_id) WHERE status = 'running';

-- 修掉 295 留下的过时口径说明：语言不再一律为空串，意图也不再只有依赖重建一种。
COMMENT ON COLUMN build_jobs.lang IS '构建语言（完整语言码，如 zh-CN / en-US）；空串 = 未冻结语言：ARCH-04 之前入队的存量任务按站点启用语言集合处理';
COMMENT ON COLUMN build_jobs.intent IS '构建意图：manual=人工发起（只构建、不回写线上），dependency=依赖失效自动重建（构建 + 按旧发布范围回写线上）';
COMMENT ON COLUMN build_jobs.build_input_hash IS '入队时冻结的构建输入版本（页面侧为草稿文档摘要）；它与 lang / intent 一起构成待办去重键，不是隐藏操作码';
