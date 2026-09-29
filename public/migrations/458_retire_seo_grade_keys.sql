-- 458 · 退役孤儿词条 admin.seo.grade.*（5 个 key / 10 行）
--
-- 与 418 / 419 同类缺陷（key 在库里、代码里没有引用者），但成因不同：本批不是「功能下线」，
-- 而是**同一批展示文案存在两份定义**被收编成一份 ——
--   · admin.seo.grade.{green,lightgreen,yellow,red,redBlocking}          ← 此前只有 product 侧在用
--   · admin.seo.score.grade.{excellent,good,needsWork,poor,missing}      ← content / project 在用（迁移 451）
-- 两批词条逐字相同（本机库实测：优秀 / Excellent、良好 / Good、需改进 / Needs work、
-- 差 / Poor、缺失 / Missing），差别只在 key 名。
--
-- product 侧原先自带 map[颜色]key + map[颜色]中文兜底（internal/module/product/inbound/http/
-- product_page_shared.go）与配套的 enums 常量（productenums.SEOGrade*），本批把它们一并删除、
-- 改调共享出口 seoscore.ScoreGradeText（internal/seo/score_grade.go）—— 于是取词落到
-- admin.seo.score.grade.*，**显示结果不变**（两批值逐字相同），这 5 个 key 成为孤儿。
--
-- 核实无引用的命令（本批执行时，除本迁移自身与 449 的 seed SQL / 门槛列表外为空）：
--   rg -n 'admin\.seo\.grade\.' .
--
-- 与 449（这 10 行的 seed）的关系 —— AGENTS.md「删能力时要连 seed 的 SQL 与幂等条件一起收口」：
--   · 449 的幂等条件逐条枚举本批自己的 142 个 key、阈值 COUNT(*) = 284；本批把 5 个 key 移出
--     列表、阈值 284 → 274（register_product_inventory_go_texts_i18n.go 同批改），使条件不再
--     依赖这 10 行 —— 否则删完之后计数永远差 10，449 每轮启动都会重跑。
--   · 449 的 **SQL 保持原样不动**（历史迁移 SQL 不改），即它内部仍有这 10 行 INSERT：一旦 449
--     因故重跑（其它 137 个 key 有缺失），这 10 行会被重新插回来。所以本批的删除注册在
--     **seed 台账**、版本 458 > 449，保证同一次 RunSeeds 内「插入在前、删除在后」，
--     每轮启动的净结果恒为「这些 key 不在库里」。
--     （若挂在 Migration 台账：Migrations 全部先跑、Seeds 后跑，删除永远排在 449 之前，
--     449 重跑那一轮结束时库里会带着这 10 行孤儿，要等下一轮启动才清 —— 122 / 104 的故障形态。）
--
-- 删除判据逐条列举这 5 个 key（不用 LIKE 前缀，见 AGENTS.md §数据库·迁移）：
-- 前缀 `admin.seo.grade.%` 将来若有别的批次写入同前缀 key，删除会越权波及。
--
-- 幂等：DELETE 本身幂等（重复执行影响 0 行）；是否执行的判定方向见注册文件的注释。

DELETE FROM sys_i18n WHERE item_key IN (
    'admin.seo.grade.green',
    'admin.seo.grade.lightgreen',
    'admin.seo.grade.yellow',
    'admin.seo.grade.red',
    'admin.seo.grade.redBlocking'
);
