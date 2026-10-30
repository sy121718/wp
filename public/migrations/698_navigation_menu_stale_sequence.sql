-- 698 · menu_stale 可靠入队的持久化形状（构建队列侧）。
--
-- 背景：导航（页眉 / 页脚 × 桌面 / 移动四位置）变化后，凡把该位置烘进产物的页面与
-- 发布实例都会停在旧字节。入队必须与触发它的业务写入**同事务**，且每一次真实变化都要
-- 分配一个严格更大的输入版本 —— 否则 A→B→A 的来回改动作会被队列里的旧任务去重吞掉。
--
-- 本迁移只加队列侧的形状，不做任何回填：
--   · build_job_seq —— 每 (工程, 菜单位置) 一行的版本号源，靠原子 UPSERT 递增取号；
--   · build_jobs.source_id 由 uuid 放宽为 text —— menu_stale 的来源是
--     「规范工程 UUID ':' 菜单位置」，uuid 列存不下（见第 2 段的取舍说明）；
--   · build_jobs.input_version / context_key —— menu_stale 的输入版本与去重语境；
--   · build_jobs_source_type_check 放宽到含 menu_stale。
--
-- 幂等：表/列/索引用 IF NOT EXISTS，约束先 DROP 再 ADD，可重复执行。
-- 不加 BEGIN/COMMIT：迁移器（ApplyStatements）已把整批语句包在同一个事务里，
-- 文件内再开事务会与外层事务冲突。
-- 存量来源（page / presentation）**不**回填 input_version：它们没有「按位置单调递增」
-- 的输入号，填 0 会与真实版本号混淆；该列对它们保持 NULL。

-- 1) 版本号源：主键 (project_id, kind) 就是「每工程每位置一行」。
--    project_id 带外键与 RLS（本表不参与跨工程队列领取，与 build_jobs 的豁免理由不同：
--    它只被同一条事务内的写入按工程取号，因此照常 fail closed）。
CREATE TABLE IF NOT EXISTS build_job_seq (
    project_id   uuid        NOT NULL
                 CONSTRAINT build_job_seq_project_id_fkey REFERENCES projects(id) ON DELETE CASCADE,
    kind         varchar(32) NOT NULL,
    -- 下一次分配出去的号。首次插入给 1，之后每次 UPSERT +1 并 RETURNING。
    next_version bigint      NOT NULL DEFAULT 1,
    create_time  timestamptz NOT NULL DEFAULT now(),
    update_time  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT build_job_seq_pkey PRIMARY KEY (project_id, kind),
    -- 四值与迁移 285 的 navigations_kind_check 一致：桌面 / 移动 × 页眉 / 页脚。
    CONSTRAINT ck_build_job_seq_kind
        CHECK (kind IN ('header', 'header_mobile', 'footer', 'footer_mobile'))
);

COMMENT ON TABLE build_job_seq IS 'menu_stale 的输入版本号源：每 (工程, 菜单位置) 一行，事务内原子 UPSERT 递增取号';
COMMENT ON COLUMN build_job_seq.next_version IS '上一次分配出去的输入版本号；每次真实变化 +1，与任务行同事务提交（回滚即未消耗）';

ALTER TABLE build_job_seq ENABLE ROW LEVEL SECURITY;
ALTER TABLE build_job_seq FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS project_isolation ON build_job_seq;
CREATE POLICY project_isolation ON build_job_seq
    USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
    WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid);

-- 2) source_id 由 uuid 放宽为 text（菜单变化的来源必须能表达「工程 + 位置」）。
--
--    为什么是 text 而不是「退回去只存裸工程 uuid」：uq_build_jobs_running_source
--    (source_type, source_id) WHERE status='running' 是本队列的**来源互斥**判据。若
--    menu_stale 的 source_id 只存裸工程 id，四个菜单位置就共用同一个来源 —— 页眉一旦在跑，
--    页脚就再也领不到（claim 的 NOT EXISTS 前置过滤 + 唯一索引一起把它挡在队列里），
--    四位置各自独立重建的需求当场失效。把位置编进 source_id 才能让互斥粒度停在「位置」。
--
--    对 page / presentation 是纯类型放宽：列上的值本来就是 uuid，::text 只是换存储形态，
--    逐值等价，没有值变化、没有 NULL 风险（列是 NOT NULL）。唯一看得见的变化是文本形式
--    由 PG 的 uuid 输出规则决定（小写带连字符的 36 字符规范形式）：Go 侧 page / presentation
--    的 id 都由 uuid.NewString() 生成，但**入队入参在改型前不做归一**（Enqueue 只做
--    TrimSpace），所以从本迁移起由 build/service 的 Enqueue 统一 uuid.Parse().String()
--    归一（见 canonicalSourceID）：非规范形态（大写、无连字符）在 uuid 列里本来会被 PG
--    归一，改成 text 之后不再归一 —— 不归一就会让同一实体的两种写法在
--    uq_build_jobs_running_source / uq_build_jobs_pending 上各成一条键，running 互斥与
--    待办去重静默失效。非法 UUID 在入队时受控拒绝（400），不会进库。
--    menu_stale 走的是另一条专用入口 EnqueueMenuStaleTx，它同样在 dto 层用
--    MenuStaleSourceID 做归一；两个入口共用的口径是「source_id 落库前必是规范文本」。
--    uq_build_jobs_pending / uq_build_jobs_running_source 由 PG 在建列改型时自动重建：
--    重建用的是原定义（键列与 predicate 逐字不变，只是键列的类型跟着表走），所以下面没有
--    任何 DROP / CREATE INDEX 去"顺手重写"它们 —— 手写重建才是把 predicate 改错的地方。
--    这不是约定而是机制：键列改型时 PG 必须重建索引，不重建的话键上的比较函数与存储
--    类型会与列的类型不一致；重建读的是 pg_index 里存的原定义，所以 predicate 不会被改写。
--    复核放在 public/test/build/feature/build_menu_stale_test.go 的
--    TestMenuStaleLegacyIndexesKeepPredicates：直接读 pg_index 的键列与 indpred，
--    不拿 snapshot / CURRENT.sql 当判据。
--
--    改型会重写整表并重建依赖本列的两条唯一索引；期间对该表的写入被锁住，读不受影响。
--    若有长事务在途会排队等待，本迁移随迁移器事务一起提交或回滚，不留半改状态。

-- 本段可重复执行：已经是 text 就不再改型（PG 对同型 ALTER 会直接报错，不是空操作）。
DO $$
BEGIN
    IF (SELECT data_type FROM information_schema.columns
         WHERE table_schema = current_schema()
           AND table_name = 'build_jobs'
           AND column_name = 'source_id') = 'uuid' THEN
        ALTER TABLE build_jobs ALTER COLUMN source_id TYPE text USING source_id::text;
    END IF;
END $$;

COMMENT ON COLUMN build_jobs.source_id IS '任务来源 id：page / presentation 为来源实体 uuid 的文本形式；menu_stale 为「规范工程 UUID:菜单位置」';

-- 2b) 改型后的回填补偿（295 的回填若仍是裸比较，在 698 之后重跑会失败）。
--
-- 迁移器按**数字升序**执行（migrator.go 的 All() → compareVersionCmp），699 → 698 → …
-- → 295；295 的 CheckSQL（register_build_lease.go）不认 source_id 的列型，所以一个从未
-- 应用过 295 的库在这一步会重跑 295。它的两条回填已经改成 source_id::text = id::text
-- （两端显式 cast，698 前后的列型都成立），下面这段不是它的替代品，而是同一件事的
-- **前移**：让升级路径上的历史行在 698 这一步就拿到工程作用域，295 再跑时按
-- project_id IS NULL 不再命中，回填的先后也就与「295 是否重跑」解耦。
-- 幂等：只处理 project_id IS NULL 的行；source_id 已有显式 ::text，两个列型阶段都能执行。
DO $$
BEGIN
    IF to_regclass('pages') IS NOT NULL THEN
        UPDATE build_jobs j
           SET project_id = p.project_id
          FROM pages p
         WHERE j.project_id IS NULL
           AND j.source_type = 'page'
           AND j.source_id::text = p.id::text;
    END IF;
    IF to_regclass('presentation_instances') IS NOT NULL THEN
        UPDATE build_jobs j
           SET project_id = i.project_id
          FROM presentation_instances i
         WHERE j.project_id IS NULL
           AND j.source_type = 'presentation'
           AND j.source_id::text = i.id::text;
    END IF;
END $$;

-- 3) 任务的输入版本与去重语境。
--    input_version 可空：只有 menu_stale 有意义，历史来源保持 NULL（不做 NOT NULL 回填）。
--    context_key 是「工程:位置」这段去重语境的稳定文本，与 uq_build_jobs_context_version
--    一起把「同一位置的同一版本只排一次」写进数据库。
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS input_version bigint NULL;
ALTER TABLE build_jobs ADD COLUMN IF NOT EXISTS context_key text NULL;

COMMENT ON COLUMN build_jobs.input_version IS 'menu_stale 的输入版本号（来自 build_job_seq）；其余来源为 NULL';
COMMENT ON COLUMN build_jobs.context_key IS 'menu_stale 的去重语境（工程 UUID:菜单位置）；其余来源为 NULL';

-- menu_stale 的四段事实必须自洽：版本号、语境键、工程、位置缺一不可。
-- 判据全部用**内置**的字符串函数，不新增自定义函数（迁移只加形状，不往库里装逻辑）：
--   · 形状：'<工程 uuid 规范文本 36 字符> : <位置>' —— 位置取 source_id 第 38 字符起；
--   · 位置封闭四值：与 ck_build_job_seq_kind 逐字一致。位置是封闭枚举、不含 ':'，
--     所以「四值命中」本身就等价于 dto.SplitMenuStaleSourceID 的「恰好两段」判据；
--   · 工程一致：source_id 的前 37 位（uuid 文本 + ':'）必须等于本行 project_id::text + ':'；
--   · 语境键 = 同一个表达式拼出来（等价于 source_id 全文），杜绝「同一条任务两个语境」。
--     CHECK 是逐行判定的纯表达式，写成等式而不是调用函数，是为了让约束定义能自解释。
ALTER TABLE build_jobs DROP CONSTRAINT IF EXISTS build_jobs_menu_stale_shape_check;
ALTER TABLE build_jobs ADD CONSTRAINT build_jobs_menu_stale_shape_check
    CHECK (
        source_type <> 'menu_stale'
        OR (
            input_version IS NOT NULL
            AND context_key IS NOT NULL
            AND project_id IS NOT NULL
            AND left(source_id, 37) = project_id::text || ':'
            AND substr(source_id, 38) IN ('header', 'header_mobile', 'footer', 'footer_mobile')
            AND context_key = left(source_id, 37) || substr(source_id, 38)
        )
    );

-- 4) 同一语境同一版本只排一次。
--    两条既有索引的实际定义（回读 307_build_jobs_intent.sql:38-40 与 295_build_jobs_lease.sql:61-62，
--    不是凭印象）：predicate 都是 **status 维**，与哪一行无关：
--      · uq_build_jobs_pending (source_type, source_id, build_input_hash, lang, intent) WHERE status = 'pending'
--      · uq_build_jobs_running_source (source_type, source_id)                         WHERE status = 'running'
--    本索引的 predicate 是 **source_type 维**（WHERE source_type = 'menu_stale'），
--    所以三者的行集合确实会交叉：一条 menu_stale 的 pending 行同时被 uq_build_jobs_pending
--    和本索引管。正确的判据不是「predicate 不重叠」，而是**键里的列不同、判的不是同一件事**：
--      · uq_build_jobs_pending 按 (来源, 输入 hash, 语言, 意图) 去重 —— 它认为
--        「同样的输入 hash 就是同一份工作」，而 menu_stale 的输入 hash 里带着版本号，
--        同一版本重复入队会被它挡下（这是期望的），不同版本各自成键（也是期望的）；
--      · 本索引按 (context_key, input_version) 去重 —— 语境是「工程:位置」、版本是号源
--        分配的号，它把「同一位置的同一版本只能排一次」写进库；即使调用方把
--        build_input_hash 传成别的值（比如换了 hash 方案），也排不出第二条同版本任务。
--    换句话说：本索引是**与 hash 形态无关**的那条底线，不是前者的替代品。
--    running 互斥仍由 uq_build_jobs_running_source 按 (source_type, source_id) 负责 ——
--    source_id 已把位置编进去，互斥粒度正好是「每工程每位置（每个 kind）」。
CREATE UNIQUE INDEX IF NOT EXISTS uq_build_jobs_context_version
    ON build_jobs (context_key, input_version)
 WHERE source_type = 'menu_stale';

-- 5) 来源类型白名单加 menu_stale（约束名由 init_builder_schema 的内联 CHECK 产生，
--    迁移 201 之后仍在位，见 schema/CURRENT.sql 的 build_jobs_source_type_check）。
ALTER TABLE build_jobs DROP CONSTRAINT IF EXISTS build_jobs_source_type_check;
ALTER TABLE build_jobs ADD CONSTRAINT build_jobs_source_type_check
    CHECK (source_type IN ('page', 'presentation', 'menu_stale'));
