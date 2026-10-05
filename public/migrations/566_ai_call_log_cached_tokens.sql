-- 566 · ai_call_log 补「缓存命中 token」两列（docs/16 §3.1 的第一个验收数字）。
--
-- 背景：537 建表时记了 input / output / total 三个数，够回答「烧了多少 token」，
--   但回答不了「这些 token 里有多少走了上游的前缀缓存」—— 而后者才是判断
--   **稳定前缀纪律（§3）有没有生效**的唯一观测：前言写得再对，只要每轮前缀都变，
--   命中率就会掉到接近 0，而账面上 token 数只多不少、没有任何报错。
--
-- 两列而不是一列：
--   cached_tokens   —— 上游报的命中数；
--   cached_reported —— 上游这次到底报没报这个字段。
--   「没报」与「报了 0」在统计上必须分开：把没报的当成 0 会拉低命中率，
--   而失真的方向恰好是「看起来更差」—— 看到难看的数字第一反应是去调提示词，
--   不会想到是上游没报。两家的字段分别是
--   usage.prompt_tokens_details.cached_tokens（chat/completions）与
--   usage.input_tokens_details.cached_tokens（responses）。
--
-- 口径与 537 的三列一致：**只记上游上报的值**，不做本地估算补齐。
--
-- 幂等：ADD COLUMN IF NOT EXISTS（本表是观测数据，不涉及回填 —— 历史行那两列为
--   false/0，语义正是「那时候我们不知道」，而这是事实）。

ALTER TABLE ai_call_log
    ADD COLUMN IF NOT EXISTS cached_tokens   BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cached_reported BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN ai_call_log.cached_tokens IS '输入里命中上游前缀缓存的 token 数（上游上报值，未报为 0）';
COMMENT ON COLUMN ai_call_log.cached_reported IS '上游是否报告过缓存命中数（false 时 cached_tokens 的 0 无意义）';
