-- 071 · 依赖 fan-out：反查索引 + dependency_kind 约束扩展（PIPE-3）
--
-- 背景：page_dependencies / presentation_dependencies 两张表此前**没有任何 Go 侧写入
-- 路径**（全仓 grep 无 INSERT，见 internal/module/*/model），失效标记一律退化为
-- 「UPDATE pages SET stale = true」全站标记（MarkStaleForTheme / MarkStaleForBlock /
-- MarkStaleForI18n）。PIPE-3 把依赖记录落库，改为按 (dependency_kind, dependency_key)
-- 反查受影响的**具体产物/页面**。两处结构缺口必须先补：
--
--   ① 反查方向缺索引。两表现有索引只有主键 (artifact_id, dependency_kind,
--      dependency_key)，而 fan-out 查询是「按 kind+key 找 artifact」——与主键前导列
--      相反，planner 只能全表扫（依赖表随产物数量线性增长，是热查询）。
--      补 (dependency_kind, dependency_key) btree。
--
--   ② dependency_kind 的 CHECK 是 docs/03-pipeline.md §8.2 的 8 值封闭枚举，不含
--      实现里真实存在的两类构建期依赖：
--        · 'i18n'  —— 构建期取词（Manifest 已在用，internal/pipeline/artifact.go
--                      DependencyKindI18N；改词条/译文需重建产物）
--        · 'block' —— 页眉/页脚全局块内联进产物（改块内容需重建）
--      不扩展就无法把这两类依赖写入依赖表（写入即违反约束），精确 fan-out 会漏
--      「块变更」「词条变更」两个来源。扩展是**加值**，不删除也不重命名任何现有值。
--
-- 幂等：索引 IF NOT EXISTS；约束先 DROP IF EXISTS 再 ADD（重复执行安全）。
-- register.go 的 CheckSQL 同时校验 2 个索引 + 2 个约束的 i18n 值，
-- 避免「索引已建、约束未扩展」的部分完成状态被默认「表存在即跳过」误判为已完成。

-- ---------------------------------------------------------------------------
-- 1) fan-out 反查索引（两表同一形状）
CREATE INDEX IF NOT EXISTS idx_page_deps_lookup
    ON page_dependencies (dependency_kind, dependency_key);

CREATE INDEX IF NOT EXISTS idx_pres_deps_lookup
    ON presentation_dependencies (dependency_kind, dependency_key);

-- ---------------------------------------------------------------------------
-- 2) dependency_kind 约束扩展（+ i18n / block）
ALTER TABLE page_dependencies
    DROP CONSTRAINT IF EXISTS page_dependencies_dependency_kind_check;

ALTER TABLE page_dependencies
    ADD CONSTRAINT page_dependencies_dependency_kind_check CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime',
        'i18n', 'block'
    ));

ALTER TABLE presentation_dependencies
    DROP CONSTRAINT IF EXISTS presentation_dependencies_dependency_kind_check;

ALTER TABLE presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_dependency_kind_check CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime',
        'i18n', 'block'
    ));
