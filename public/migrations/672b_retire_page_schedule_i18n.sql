-- 672b · 定时上下线（PIPE-7）下线：词条清理（数据侧）。
--
-- 460a 曾 seed 35 个 key（业务错误 6 / 成功回执 2 / 列表徽标与入口 3 / 面板 19 /
-- 排定状态 5，中英共 70 行）。代码与模板都已删除，这些词条不再有任何引用点 ——
-- 留着它们会让「词条页」出现一批点了也没地方用的空壳（同 636b 的取舍）。
--
-- 与 460a 同一条教训（两个方向都要闭合）：
--   · 用 LIKE 前缀删：将来若有别的批次用到同前缀 key，会被一起误删；
--   · 用全库总量判：永远追不平，每次启动重跑。
-- 所以下面**逐条枚举本批要清的 35 个 key**，不带通配。
--
-- 幂等：DELETE 是条件删除，重复执行影响 0 行。

DELETE FROM sys_i18n
 WHERE item_key IN (
    'ErrScheduleActionInvalid', 'ErrScheduleApplyFailed', 'ErrScheduleInPast',
    'ErrScheduleNotFound', 'ErrScheduleOccupied', 'ErrScheduleRunning',
    'MsgScheduleCanceled', 'MsgScheduleSet', 'admin.pages.action.schedule',
    'admin.page.schedule.action', 'admin.page.schedule.action.offline', 'admin.page.schedule.action.publish',
    'admin.page.schedule.cancel', 'admin.page.schedule.col.action', 'admin.page.schedule.col.actions',
    'admin.page.schedule.col.lang', 'admin.page.schedule.col.note', 'admin.page.schedule.col.status',
    'admin.page.schedule.col.time', 'admin.page.schedule.empty', 'admin.page.schedule.lang',
    'admin.page.schedule.lang_hint', 'admin.page.schedule.note', 'admin.page.schedule.redirect',
    'admin.page.schedule.redirect_hint', 'admin.page.schedule.status.canceled', 'admin.page.schedule.status.done',
    'admin.page.schedule.status.failed', 'admin.page.schedule.status.pending', 'admin.page.schedule.status.running',
    'admin.page.schedule.submit', 'admin.page.schedule.time', 'admin.page.schedule.title',
    'admin.pages.status.scheduled', 'admin.pages.status.schedule_failed'
 );
