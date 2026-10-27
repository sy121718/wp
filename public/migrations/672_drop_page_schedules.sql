-- ========================================
-- 672 · 定时上下线（PIPE-7）整体下线：表与权限点（结构侧）
--
-- 背景（2026-10 用户拍板）：「定时完全用不上」。原实现把「什么时候对该页面的哪个语言
-- 做什么」做成一张可认领、可审计的待办表（460），外加每分钟一次的到点扫描 ——
-- 运营的实际用法是手动发布 + 手动下线，这条链路只带来一份长期为空的表、
-- 一个永不触发失败的 ticker，和列表里一个点开也没事可做的按钮。
--
-- 代码侧已同批删除（service / model / inbound/http / dto / enums / 片段模板 /
-- 权限点三条 / 页面列表的行内徽标与入口），本迁移只负责让存量库的形态跟上：
--   1. page_schedules 整表（460 建的三条索引随表消失）；
--   2. page:schedule_{set,cancel,list} 三个权限点与它们指向的三条 API 策略。
--
-- 为什么 DDL 写在 **Migration** 台账、而词条清理写在 **seed** 台账（672b）：
--   与 636 / 636b 同一取舍 —— 结构变更必须在 Migrations（RunSeeds 恒在其后），
--   sys_i18n 词条是数据，历史 seed（460a）写过它们，删除必须排在其后。
--
-- 为什么不改历史迁移（460 建表、506 补索引）：迁移是变更历史，只追加不改写；
-- 新库由本迁移把结果改到终态，存量库由本迁移补齐。460 / 460a / 506 三个**纯定时**
-- 迁移已随本次改动整文件删除（内容全部是本迁移要抹掉的东西），故 DROP 全部带 IF EXISTS。
--
-- 幂等：DROP 带 IF EXISTS，DELETE 是条件删除，重复执行影响 0 行。
-- ========================================

-- ---- 1. 待办表整表下线 ----
--
-- 三条二级索引（idx_page_schedules_due / idx_page_schedules_lease /
-- uq_page_schedules_active）与 506 的 idx_page_schedules_page_created 随表一起消失，
-- 不需要单独 DROP INDEX。

DROP TABLE IF EXISTS page_schedules;

-- ---- 2. 权限点：三条 page:schedule_* + 它们指向的三条 API 策略 ----
--
-- 先删策略再删权限点：sys_permission 被删后策略行会变成指向不存在权限码的死授权
--（与 122 / 636 的处理同形；codes.go 的声明幂等 upsert 已同批删掉，不会再插回）。

DELETE FROM sys_casbin_rule
 WHERE v1 IN ('/api/page/schedule/set', '/api/page/schedule/cancel', '/api/page/schedule/list');

DELETE FROM sys_permission
 WHERE permission_code IN ('page:schedule_set', 'page:schedule_cancel', 'page:schedule_list');
