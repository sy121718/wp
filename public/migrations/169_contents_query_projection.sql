-- 169_contents_query_projection.sql
--
-- 审计 DB-024（JSONB 使用复核）与 PERF-008（集合源先取 100 条大 JSONB 再内存过滤）。
--
-- contents.data 存文章全部字段（含 body 全文）。两个后果：
--   1. 按标题检索 `data->>'title' ILIKE '%x%'` 只能全表扫（JSONB 表达式无索引）；
--   2. 集合源先取回整行再把 data 展开成字段表，正文全文白读一遍。
--
-- 处置分两半，本迁移负责数据库那一半：把 title 提成生成列并给索引；
-- 另一半（查询列投影 + 筛选下推）在 content 模块的 model 层。
-- 用生成列而不是「加普通列 + 改写入路径」：STORED 生成列由数据库维护，
-- 现有写入代码（含手工 SQL 与种子）一行都不用动，也不可能写出与 data 不一致的 title。

-- ── 1. title 生成列 ───────────────────────────────────────────────────────────
-- data->>'title' 为 NULL（键缺失）时生成列同样是 NULL —— 与 JSONB 的语义一致，
-- 不需要额外兜底。GENERATED ALWAYS ... STORED 不可直接 ALTER 成普通列，
-- 但本列没有任何写入方，迁移风险只在「加列时重写一次表」（contents 行数有限）。
ALTER TABLE contents ADD COLUMN IF NOT EXISTS title text GENERATED ALWAYS AS (data->>'title') STORED;

COMMENT ON COLUMN contents.title IS '由 data->>''title'' 生成的查询列（审计 DB-024）：只读，供检索与排序用；写入仍只写 data';

-- ── 2. 检索索引 ───────────────────────────────────────────────────────────────
-- 必须自带 CREATE EXTENSION，不能假定「167 装过了」：扩展按 schema 解析，而安装是
-- 库级唯一的。这里显式带 SCHEMA ext_shared，保证不论谁先跑都落在同一处；不带 SCHEMA 会落进
-- 「当前 schema」，后跑的 schema 只会静默跳过，随后建 trgm 索引报 operator class does not exist。
-- 幂等：已装时是一条空操作。
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA ext_shared;

CREATE INDEX IF NOT EXISTS idx_contents_title_trgm ON contents USING gin (title gin_trgm_ops);

-- ── 3. 列表与集合源的排序索引 ─────────────────────────────────────────────────
-- 集合源与后台列表都是「按类型取数、按 updated_at 倒序、id 兜底」的确定性排序
-- （同一批数据每次构建必须输出同样字节，不变量 5）：三列一起建才走得动。
CREATE INDEX IF NOT EXISTS idx_contents_type_updated ON contents (entity_type, updated_at DESC, id DESC);
