-- 308 · page_publication_plans：发布计划冻结（审计 I18N-01）。
--
-- 背景：手工 Page 的 hreflang 互指与「默认语言是谁」是**构建期**从站点语言清单
-- （project_locales）推导出来的，而清单是可编辑配置。产物一旦建成，任何一次重放 /
-- 重建都会用当时那份清单重算一遍：发布前的确定性复构建、组件升级后的批量重建
-- （page.RebuildStale）、灾难恢复的按元数据重建（page.RebuildArtifact）。
-- 于是同一份冻结源文档在配置改动之后产出另一份互指链接 —— 既有产物的 hreflang
-- 凭空变了（少一条、多一条、x-default 换人），而线上路径一个都没动。
--
-- 本表把「这次发布依据哪几种语言、默认语言是谁」固化成发布事实：
--   · 首次构建 / 草稿改动后的构建时冻结一次（pipeline.PublicationPlan）；
--   · 此后一切重编译一律以冻结值为准，不再回读 project_locales。
-- 产物侧另有一份等价快照（Manifest.siteLangs / siteDefaultLang），供「按某一份具体
-- 产物重建」的场景复现它当时依据的输入。
--
-- 键与 page_stagings / page_publications 同形（page_id, lang），三个台账各自回答一个
-- 问题：暂存了什么、激活了什么、依据哪份语言输入。刻意**不加外键**（与这两张兄弟表
-- 一致）：它们是发布流水而不是业务实体，硬失败会卡住「页面已删、台账待回收」的清理。
--
-- 无 project_id：page_stagings / page_publications 同样没有，三张表同属本模块内部
-- 台账、不在迁移 215 的 RLS 清单里 —— 口径一致才不会出现「同一张表读写在两种作用域
-- 下行为不同」。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / 索引 IF NOT EXISTS，重复执行安全。
-- 注册见 register_publication_plan.go：本表为新表，用默认「表存在即跳过」检查即可。

CREATE TABLE IF NOT EXISTS page_publication_plans (
    page_id       uuid        NOT NULL,
    lang          text        NOT NULL,
    -- 冻结的发布计划原文：{"siteLangs": [...], "defaultLang": "..."}。
    -- 整份 JSON 而不是拆成多列：计划是会加字段的（目标语言、canonical origin、
    -- 构建依赖版本都属后续批次），拆列意味着每加一个输入都要再来一次迁移。
    plan          jsonb       NOT NULL,
    -- 规范化计划的内容指纹（pipeline.PublicationPlan.Hash）：让「计划换没换」
    -- 可以在 SQL 里核对，不必解 JSON。
    plan_hash     text        NOT NULL DEFAULT '',
    -- 冻结时的草稿版本：草稿被改动 = 新的发布决策，下一次构建按当前配置重新冻结。
    -- 判据刻意不是「配置变了」——那等于没有冻结。
    draft_version bigint      NOT NULL DEFAULT 0,
    create_time   timestamptz NOT NULL DEFAULT now(),
    update_time   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (page_id, lang)
);

-- 按 lang 删（禁用语言时该语言的计划随之退役，见 page.RetireLocale）。
CREATE INDEX IF NOT EXISTS idx_page_publication_plans_lang ON page_publication_plans (lang);

COMMENT ON TABLE page_publication_plans IS '页面每语言的发布计划冻结（审计 I18N-01）：主键 (page_id, lang)；重建入口以冻结值为准，不回读当前站点语言配置';
COMMENT ON COLUMN page_publication_plans.plan IS '冻结的发布计划 JSON：siteLangs（默认语言在前）+ defaultLang';
COMMENT ON COLUMN page_publication_plans.plan_hash IS '规范化计划的内容指纹（pipeline.PublicationPlan.Hash）';
COMMENT ON COLUMN page_publication_plans.draft_version IS '冻结时的草稿版本；草稿改动后下一次构建重新冻结';
