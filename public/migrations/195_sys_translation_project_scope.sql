-- 195_sys_translation_project_scope.sql
--
-- 审计 I18N-009（low / design-flaw）：sys_translation 没有 project_id，
-- 多站点（多工程）场景下两个工程有相同原文时会共用同一条译文，无法隔离。
--
-- 现状核对：066 建表时主键为 (source_hash, context, lang)，确实没有工程维度，
-- 与审计前提一致（066 文件头也写着「无 project_id：跨页面/跨站点复用是全局的」）。
--
-- ── 落地方式：在当前表上加可空的 project_id（而非审计原文建议的独立覆盖表）──────
-- 审计原文的 remediation 是「不改主键，另建 sys_translation_override 覆盖表，
-- 先查覆盖表再回落全局表」。本批改为在当前表加列，理由：
--   1. 覆盖表要两次批量查询 + 应用层 merge；单表 + 作用域唯一键用**一条 SQL**
--      就能拿到「工程行优先、全局行回落」（SELECT DISTINCT ON，见
--      pkg/i18n/content_store.go 的工程级查询形态），构建期仍是「零查库」；
--   2. 覆盖表会产生两处写入口（工作台保存到底写哪张？），单表写入仍是一次 ON CONFLICT；
--   3. 审计担心的「改主键会失去内容寻址的好处」在本批并不成立：内容寻址
--      （按 sha256(source_text) 定位、改原文自动失效）逐字保留，只是把「作用域」
--      从一个隐式的全局变成显式的 (project_id, source_hash, context, lang)；
--      工程作用域为 NULL 时语义与今天完全一致（见取舍 A）。
--
-- ── 取舍 A：既有行填什么 —— **NULL = 全局共享**，不回填到默认工程 ─────────────────
--   1. 066 的既有语义就是全局（决策 F11/D12「跨页面/跨站点复用是全局的」）：
--      既有行保持 NULL，升级后任何工程的构建结果逐字节不变（零行为回归）。
--      若回填到「默认工程」，其它工程立刻读不到这些译文，是静默的功能倒退；
--      而且迁移期间没有任何信息能判断「这条译文当初是为哪个站点翻的」。
--   2. 迁移不依赖 projects 表的内容：库为空（一个工程都没有）时也能完整执行。
--   3. NULL 表示「全局共享」在本项目已有先例：inventory 变动原因字典的内置原因
--      就是 project_id IS NULL 全工程可见。
--   4. 反例明确排除：直接 NOT NULL 且不给默认值会让有数据的库迁移失败。
--
-- ── 取舍 B：唯一性用 NULLS NOT DISTINCT（要求 PostgreSQL >= 15）─────────────────
--   原主键 (source_hash, context, lang) 使「同 key 不同工程并存」不可能，必须换成
--   含 project_id 的唯一键。三种写法里选 NULLS NOT DISTINCT：
--     · 普通唯一索引：NULL 互不相等 ⇒ 全局行可插入任意多条同 key 行（语义被破坏）；
--     · 哨兵 UUID 表示全局：要占一个假 uuid，且无法再挂 projects 外键
--       （删工程不会级联清掉该工程的译文）；
--     · NULLS NOT DISTINCT：NULL 在唯一性判定里「等于 NULL」，
--       ON CONFLICT (project_id, source_hash, context, lang) 可直接推断该索引。
--       已在 PostgreSQL 18 实测：全局行 upsert 命中同一行并更新；三个作用域
--       （NULL / 工程1 / 工程2）同 key 并存互不冲突；工程行压过全局行、未翻译的
--       工程回落全局行。
--   本项目部署基线是 postgres:18-alpine（docker-compose.yml），满足该要求。
--   若将来必须支持 PG < 15：等价改法是 COALESCE(project_id, '00000000-…'::uuid)
--   表达式唯一索引 + 应用层两段式写（先 UPDATE 后 INSERT）——本批不采用，
--   因为表达式索引无法被 ON CONFLICT 推断，会把幂等写入从一条 SQL 拆成两段代码。
--
-- ── 取舍 C：DROP 旧主键、不新建主键 ─────────────────────────────────────────────
--   PG 的主键列隐式 NOT NULL，而「全局行」必须允许 project_id 为 NULL，故
--   (project_id, source_hash, context, lang) 不能做主键。唯一性由
--   uq_sys_translation_scope_key 承担；sys_translation 不被任何表引用，
--   丢掉「主键」这个物理标识不影响任何调用方（写入走 ON CONFLICT 列推断，
--   读取走 source_hash 相关索引）。
--
-- ── 历史重复行 ──────────────────────────────────────────────────────────────
--   新唯一索引在既有数据上一定建得成功：旧主键保证 (source_hash, context, lang)
--   整表唯一，而既有行全部是 project_id IS NULL，落到新索引上仍是同一个键。
--
-- 幂等：迁移器逐语句执行且不包事务（public/migrations/migrator.go），故每条语句
--   都必须可重复执行：ADD COLUMN IF NOT EXISTS / DROP CONSTRAINT IF EXISTS /
--   CREATE INDEX IF NOT EXISTS / 外键用 DO 块判 pg_constraint。

-- 1) 工程作用域列：NULL = 全局共享（既有行原样保留 NULL，见「取舍 A」）。
ALTER TABLE sys_translation ADD COLUMN IF NOT EXISTS project_id uuid;

COMMENT ON COLUMN sys_translation.project_id IS
    '工程作用域：NULL = 全局共享（既有行语义，跨站点复用）；非 NULL = 仅该工程使用，构建期优先于全局行';

-- 2) 去掉旧主键：含 project_id 的作用域唯一性由第 4 步的索引承担（见「取舍 C」）。
ALTER TABLE sys_translation DROP CONSTRAINT IF EXISTS sys_translation_pkey;

-- 3) 工程外键：工程删除时连带清掉它的工程级译文，不留「工程已不存在」的孤儿行。
--    ADD CONSTRAINT 不支持 IF NOT EXISTS，故用 DO 块判 pg_constraint 保证幂等。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'sys_translation_project_id_fkey'
           AND conrelid = 'sys_translation'::regclass
    ) THEN
        ALTER TABLE sys_translation
            ADD CONSTRAINT sys_translation_project_id_fkey
            FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
    END IF;
END $$;

-- 4) 作用域唯一键（取代 066 的主键）：同工程同 key 只一行；NULL 视为同一作用域，
--    全局行因此也唯一（见「取舍 B」）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_sys_translation_scope_key
    ON sys_translation (project_id, source_hash, context, lang) NULLS NOT DISTINCT;

COMMENT ON INDEX uq_sys_translation_scope_key IS
    '作用域唯一键（取代 066 的主键，审计 I18N-009）：NULLS NOT DISTINCT 让全局行（project_id IS NULL）同样唯一，ON CONFLICT 可直接推断';

-- 5) 构建期取数索引：工程级查询形态是
--    WHERE source_hash = ANY($1) AND lang = $2 AND (project_id IS NULL OR project_id = $3)
--    （project_id, source_hash, lang）起前导列命中工程行那一支；全局行那一支由 066 的
--    idx_sys_translation_hash_lang 承担，planner 可 BitmapOr 组合。
CREATE INDEX IF NOT EXISTS idx_sys_translation_project_hash_lang
    ON sys_translation (project_id, source_hash, lang);

COMMENT ON INDEX idx_sys_translation_project_hash_lang IS
    '构建期按工程取内容译文：project_id + source_hash + lang（审计 I18N-009）';
