-- 460a · 定时上下线（PIPE-7）的词条：业务错误 / 成功回执 / 后台面板文案。
--
-- 为什么与 460 同一批（编号后缀 a 而不是另起 461）：这批词条与 page_schedules 是同一件事的
-- 两半 —— 表负责「排定与执行」，词条负责「失败原因与界面文案」。同批落库、同一个提交，
-- 才能避免「迁移已上、页面显示裸 key」的中间态；编号加后缀是仓库既有表达「同批第二半」的写法
--（compareVersion 的 086 < 086a < 086b < 087）。
--
-- 词条形态与取值来源（**真源都是 page enums 的常量值**，不是模板里现编的字面量）：
--   · ErrScheduleNotFound / InPast / ActionInvalid / Running / Occupied —— service 的业务 sentinel
--     （page/service/page_errors.go），消息值 = i18n key，读侧取词后展示；
--   · ErrScheduleApplyFailed —— page_schedules.last_error 的**归口** key：到点执行遇到识别不出的
--     内部错误时写它，原文只进日志（那一列会显示在后台）。
--     另四个失败原因复用既有词条（ErrRebuildRequired / ErrNoStagedArtifact / ErrPathOccupied /
--     ErrPageNotFound），本批不重复 seed；
--   · MsgScheduleSet / MsgScheduleCanceled —— 接口成功回执；
--   · admin.page.schedule.* / admin.pages.* —— 后台面板片段与页面列表徽标的文案，
--     fallback 中文原文留在模板里（internal/templates/fragments/page_schedule_panel.html 与
--     internal/templates/admin/page/pages.html）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_page_schedule_i18n.go 里**逐条枚举本批 35 个 item_key**（上界封闭，35 × 2 = 70 行），
-- 不用 LIKE 前缀、也不用全库总量 —— 前缀判据会在「已有别的批次同前缀行」时计数虚高而静默跳过本批，
-- 全库总量判据会在将来新增同前缀 key 时永远追不平而每次启动重跑（058 / 076 两次真实故障）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— 业务错误（page/service/page_errors.go）——
('ErrScheduleNotFound', 'zh-CN', '排定不存在', 404, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleNotFound', 'en-US', 'That schedule does not exist.', 404, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleInPast', 'zh-CN', '排定时间必须晚于当前时刻', 400, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleInPast', 'en-US', 'The scheduled time must be in the future.', 400, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleActionInvalid', 'zh-CN', '排定动作不合法（只支持上线与下线）', 400, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleActionInvalid', 'en-US', 'Unsupported schedule action (only publish and offline are accepted).', 400, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleRunning', 'zh-CN', '排定正在执行，无法取消', 409, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleRunning', 'en-US', 'The schedule is already running, so it cannot be cancelled.', 409, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleOccupied', 'zh-CN', '同类排定正在执行，请稍后再试', 409, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleOccupied', 'en-US', 'A schedule with the same action is running right now; please try again later.', 409, 'error', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('ErrScheduleApplyFailed', 'zh-CN', '排定执行失败，请查看服务日志', 500, 'error', 'internal/module/page/enums/page_enums.go（page_schedules.last_error 的归口 key）', 1, now(), now()),
('ErrScheduleApplyFailed', 'en-US', 'The schedule failed to run; check the service log.', 500, 'error', 'internal/module/page/enums/page_enums.go (fallback key for page_schedules.last_error)', 1, now(), now()),
-- —— 成功回执 ——
('MsgScheduleSet', 'zh-CN', '已排定，到点自动执行', 200, 'ui', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('MsgScheduleSet', 'en-US', 'Scheduled; it will run automatically at that time.', 200, 'ui', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('MsgScheduleCanceled', 'zh-CN', '排定已取消', 200, 'ui', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
('MsgScheduleCanceled', 'en-US', 'The schedule was cancelled.', 200, 'ui', 'internal/module/page/enums/page_enums.go', 1, now(), now()),
-- —— 页面列表的行内徽标与入口（admin/page/pages.html）——
('admin.pages.status.scheduled', 'zh-CN', '已排定', 200, 'admin', 'internal/templates/admin/page/pages.html', 1, now(), now()),
('admin.pages.status.scheduled', 'en-US', 'Scheduled', 200, 'admin', 'internal/templates/admin/page/pages.html', 1, now(), now()),
('admin.pages.status.schedule_failed', 'zh-CN', '排定失败', 200, 'admin', 'internal/templates/admin/page/pages.html', 1, now(), now()),
('admin.pages.status.schedule_failed', 'en-US', 'Schedule failed', 200, 'admin', 'internal/templates/admin/page/pages.html', 1, now(), now()),
('admin.pages.action.schedule', 'zh-CN', '定时', 200, 'admin', 'internal/templates/admin/page/pages.html', 1, now(), now()),
('admin.pages.action.schedule', 'en-US', 'Schedule', 200, 'admin', 'internal/templates/admin/page/pages.html', 1, now(), now()),
-- —— 定时上下线面板（fragments/page_schedule_panel.html）——
('admin.page.schedule.title', 'zh-CN', '定时上下线', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.title', 'en-US', 'Scheduled publishing', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.action', 'zh-CN', '动作', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.action', 'en-US', 'Action', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.action.publish', 'zh-CN', '上线', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.action.publish', 'en-US', 'Publish', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.action.offline', 'zh-CN', '下线', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.action.offline', 'en-US', 'Take offline', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.time', 'zh-CN', '执行时间', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.time', 'en-US', 'Run at', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.lang', 'zh-CN', '语言', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.lang', 'en-US', 'Language', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.lang_hint', 'zh-CN', '留空 = 站点默认语言', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.lang_hint', 'en-US', 'Leave empty for the site default language', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.redirect', 'zh-CN', '下线后跳转到', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.redirect', 'en-US', 'Redirect to after going offline', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.redirect_hint', 'zh-CN', '/new-path（留空 = 直接 404）', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.redirect_hint', 'en-US', '/new-path (leave empty to serve 404)', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.note', 'zh-CN', '上线到点只切访问面指针，用排定当时构建的那份产物；排定之后草稿又被改动的话，到点不会上线，而是记一条「排定失败」（不会按新草稿重新编译）。执行时刻按站点时区理解。', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.note', 'en-US', 'At the scheduled time the access layer pointer is switched to the artifact that was built when you scheduled it — nothing is recompiled. If the draft changes after scheduling, nothing goes live; the schedule is marked as failed instead (it will not be rebuilt from the newer draft). Times are interpreted in the site time zone.', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.submit', 'zh-CN', '排定', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.submit', 'en-US', 'Schedule', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.empty', 'zh-CN', '这个页面还没有排定记录。', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.empty', 'en-US', 'This page has no schedules yet.', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.time', 'zh-CN', '执行时间', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.time', 'en-US', 'Run at', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.action', 'zh-CN', '动作', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.action', 'en-US', 'Action', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.lang', 'zh-CN', '语言', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.lang', 'en-US', 'Language', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.status', 'zh-CN', '状态', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.status', 'en-US', 'Status', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.note', 'zh-CN', '说明', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.note', 'en-US', 'Note', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.actions', 'zh-CN', '操作', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.col.actions', 'en-US', 'Actions', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.cancel', 'zh-CN', '取消', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
('admin.page.schedule.cancel', 'en-US', 'Cancel', 200, 'admin', 'internal/templates/fragments/page_schedule_panel.html', 1, now(), now()),
-- —— 排定状态（面板行内文案，由 handler 取词后渲染）——
('admin.page.schedule.status.pending', 'zh-CN', '待执行', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.pending', 'en-US', 'Pending', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.running', 'zh-CN', '执行中', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.running', 'en-US', 'Running', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.done', 'zh-CN', '已完成', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.done', 'en-US', 'Done', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.failed', 'zh-CN', '已失败', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.failed', 'en-US', 'Failed', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.canceled', 'zh-CN', '已取消', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now()),
('admin.page.schedule.status.canceled', 'en-US', 'Cancelled', 200, 'admin', 'internal/module/page/inbound/http/page_schedule_handle.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
