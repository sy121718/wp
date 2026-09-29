-- 466a · 评论模块的文案词条（BIZ-5）。
--
-- 范围（本批唯一新增词条的地方）：
--   · site.fragment.comment.*  —— 评论片段的访客文案（commentList 的表头 / 空态 / 表单，
--     commentSubmit 的四种失败与一种成功）。七种降级各有各的说法：未登录要访客去登录、
--     端口未接入要运维去接线、没给工程 / 没给实体是页面作者的配置问题、实体类型不在白名单
--     说明页面配置用了没注册的类型。它们页面上长得一样的话，装配缺陷会被当成「这站要登录」
--     而被忽略很久 —— 这正是要把它们分开的理由。
--     词条取自 internal/module/runtimefragment/enums 的 Comment* 常量（值即 item_key）。
--   · admin.comment.*          —— 后台审核页（/admin/comments）的列头 / 筛选 / 状态名 /
--     批量动作 / 空态 / 回带提示，以及菜单标题（title_key = admin.comment.menu）。
--     词条取自 internal/module/comment/enums 的 LabelKey* / Msg* 常量。
--
-- 命名与模块归属：访客端走既有 site.fragment.* 四段形态，管理端走 admin.* 三段。
-- 本批**不新增权限点 seed**（权限点由 permission.SyncToDB 在装配末尾幂等 upsert，
-- 见 internal/permission/codes.go）；菜单在 467 里 seed。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register_comment_i18n.go 里
-- **逐条枚举本批的 item_key**（上界封闭），不用 LIKE 前缀、也不用全库总量 ——
-- 前缀判据会在「已有别的批次同前缀行」时计数虚高而静默跳过本批（058 的真实故障），
-- 全库总量判据会在将来新增同前缀 key 时永远追不平而每次启动重跑（076 的真实故障）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— 片段：列表与表单的固定文案 ——
('site.fragment.comment.title', 'zh-CN', '评论', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.title', 'en-US', 'Comments', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.empty', 'zh-CN', '还没有评论，来说点什么吧。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.more', 'zh-CN', '更多评论', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.more', 'en-US', 'More comments', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.empty', 'en-US', 'No comments yet - be the first to say something.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.form_title', 'zh-CN', '写下你的评论', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.form_title', 'en-US', 'Write a comment', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.body_placeholder', 'zh-CN', '说点什么…', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.body_placeholder', 'en-US', 'Say something...', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.submit', 'zh-CN', '发表评论', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.submit', 'en-US', 'Post comment', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.reply', 'zh-CN', '回复', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.reply', 'en-US', 'Reply', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.reply_to', 'zh-CN', '回复这条评论', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.reply_to', 'en-US', 'Reply to this comment', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.author_guest', 'zh-CN', '访客', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.author_guest', 'en-US', 'Guest', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),

-- —— 片段：提交结果与五类降级 ——
('site.fragment.comment.pending_notice', 'zh-CN', '评论已提交，待审核通过后显示。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.pending_notice', 'en-US', 'Your comment was submitted and will appear once it is reviewed.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.guest', 'zh-CN', '登录后可以发表评论。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.guest', 'en-US', 'Sign in to post a comment.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.unavailable', 'zh-CN', '评论功能暂时不可用。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.unavailable', 'en-US', 'Comments are unavailable right now.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.project_missing', 'zh-CN', '这个页面还没指定站点工程，评论无法显示。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.project_missing', 'en-US', 'This page does not specify a site project, so comments cannot be shown.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.entity_missing', 'zh-CN', '这个页面还没指定评论对象（实体类型与实体 id）。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.entity_missing', 'en-US', 'This page does not specify what is being commented on (entity type and id).', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.entity_invalid', 'zh-CN', '这个页面的评论对象类型不被支持，请联系站点管理员。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.entity_invalid', 'en-US', 'Comments are not supported for this kind of content - please contact the site administrator.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.list_failed', 'zh-CN', '评论暂时读不出来 —— 稍后刷新页面再试。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.list_failed', 'en-US', 'Comments could not be loaded - refresh the page and try again.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.body_required', 'zh-CN', '评论内容不能为空。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.body_required', 'en-US', 'The comment cannot be empty.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.body_too_long', 'zh-CN', '评论太长了（最多 2000 字）。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.body_too_long', 'en-US', 'This comment is too long (2000 characters at most).', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.csrf_expired', 'zh-CN', '页面已过期，请刷新后重试。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.csrf_expired', 'en-US', 'This page has expired - please refresh and try again.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.rate_limited', 'zh-CN', '评论太频繁了，休息一会儿再发。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.rate_limited', 'en-US', 'You are commenting too fast - please wait a moment.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.submit_failed', 'zh-CN', '评论没能提交成功，请稍后重试。', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),
('site.fragment.comment.submit_failed', 'en-US', 'The comment could not be submitted - please try again later.', 200, 'ui', 'runtimefragment comment fragments', 1, now(), now()),

-- —— 管理端：审核页（列头 / 筛选 / 状态 / 动作 / 空态 / 回带） ——
('admin.comment.menu', 'zh-CN', '评论审核', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.menu', 'en-US', 'Comments', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.body', 'zh-CN', '内容', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.body', 'en-US', 'Comment', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.entity', 'zh-CN', '评论对象', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.entity', 'en-US', 'Commented on', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.author', 'zh-CN', '评论人', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.author', 'en-US', 'Author', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.status', 'zh-CN', '状态', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.status', 'en-US', 'Status', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.time', 'zh-CN', '提交时间', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.time', 'en-US', 'Submitted', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.actions', 'zh-CN', '操作', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.actions', 'en-US', 'Actions', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.status', 'zh-CN', '状态', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.status', 'en-US', 'Status', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.entity_type', 'zh-CN', '实体类型', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.entity_type', 'en-US', 'Entity type', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.keyword', 'zh-CN', '关键词', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.keyword', 'en-US', 'Keyword', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.all', 'zh-CN', '全部', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.all', 'en-US', 'All', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.search', 'zh-CN', '筛选', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.search', 'en-US', 'Filter', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.reset', 'zh-CN', '重置', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.filter.reset', 'en-US', 'Reset', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.status.pending', 'zh-CN', '待审核', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.pending', 'en-US', 'Pending', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.approved', 'zh-CN', '已通过', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.approved', 'en-US', 'Approved', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.rejected', 'zh-CN', '已驳回', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.rejected', 'en-US', 'Rejected', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.spam', 'zh-CN', '垃圾', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.status.spam', 'en-US', 'Spam', 200, 'admin', 'internal/module/comment/enums', 1, now(), now()),
('admin.comment.action.approve', 'zh-CN', '通过', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.action.approve', 'en-US', 'Approve', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.action.reject', 'zh-CN', '驳回', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.action.reject', 'en-US', 'Reject', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.empty', 'zh-CN', '没有符合条件的评论。', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.lastError', 'zh-CN', '上一次操作未完成：', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.lastError', 'en-US', 'The last action did not complete: ', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.confirmReject', 'zh-CN', '驳回选中的评论？驳回后它们不会出现在公开页面上。', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.confirmReject', 'en-US', 'Reject the selected comments? Rejected comments never appear on public pages.', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.replyBadge', 'zh-CN', '回复', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.replyBadge', 'en-US', 'Reply', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),

('admin.comment.empty', 'en-US', 'No comments match the current filters.', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.empty_hint', 'zh-CN', '换一个筛选条件，或等访客提交新的评论。', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.empty_hint', 'en-US', 'Try another filter, or wait for visitors to submit new comments.', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.hint', 'zh-CN', '评论按「站点工程 + 实体」隔离；只有「已通过」的评论会出现在公开页面上。', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.hint', 'en-US', 'Comments are scoped to one site project and entity; only approved comments appear on public pages.', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.done.approved', 'zh-CN', '已通过 %d 条评论。', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.done.approved', 'en-US', 'Approved %d comments.', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.done.rejected', 'zh-CN', '已驳回 %d 条评论。', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.done.rejected', 'en-US', 'Rejected %d comments.', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.err.nothing_selected', 'zh-CN', '没有选择任何评论。', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.err.nothing_selected', 'en-US', 'No comments were selected.', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.project_required', 'zh-CN', '请先选择站点工程：评论按工程隔离。', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.project_required', 'en-US', 'Select a site project first: comments are scoped per project.', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.no_project', 'zh-CN', '还没有站点工程：评论按工程隔离，先建一个工程再看这一页。', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.no_project', 'en-US', 'No site project yet: comments are scoped per project, so create one first.', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.load_failed', 'zh-CN', '评论列表暂时读不出来 —— 稍后重试。', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.load_failed', 'en-US', 'The comment list could not be loaded - please retry later.', 200, 'admin', 'internal/module/comment/inbound/http', 1, now(), now()),
('admin.comment.col.entity_id', 'zh-CN', '对象 id', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),
('admin.comment.col.entity_id', 'en-US', 'Entity id', 200, 'admin', 'internal/templates/admin/comment/comments.html', 1, now(), now()),

-- —— 接口响应文案（comment.err.* / comment.msg.*）——
--
-- 这些 key 由 service / handler 作为**业务错误与成功回执**返回，出口是 pkg/response
-- （JSON 信封），与片段文案（site.fragment.comment.*）不是同一条链：片段走 FacingText，
-- 接口走 TranslateMessage。两者都在 466a 里 seed，是因为它们同属「评论模块的文案」。
--
-- category 取模块名（与 masterdata.err.* / membership.err.* 的既有口径一致）。
('comment.err.invalidParam', 'zh-CN', '参数不合法。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.invalidParam', 'en-US', 'Invalid parameter.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.projectRequired', 'zh-CN', '请先选择站点工程：评论按工程隔离。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.projectRequired', 'en-US', 'Select a site project first: comments are scoped per project.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.entityTypeUnknown', 'zh-CN', '这个实体类型不支持评论（它还没有注册到评论模块）。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.entityTypeUnknown', 'en-US', 'Comments are not supported for this entity type (it is not registered with the comment module).', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.entityIDInvalid', 'zh-CN', '评论对象 id 不合法。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.entityIDInvalid', 'en-US', 'The entity id is invalid.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.bodyRequired', 'zh-CN', '评论内容不能为空。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.bodyRequired', 'en-US', 'The comment cannot be empty.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.bodyTooLong', 'zh-CN', '评论太长了（最多 2000 字）。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.bodyTooLong', 'en-US', 'This comment is too long (2000 characters at most).', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.rateLimited', 'zh-CN', '评论提交太频繁，请稍后再试。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.rateLimited', 'en-US', 'You are commenting too fast - please wait a moment.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.notAllowed', 'zh-CN', '当前不满足这条内容的评论条件。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.notAllowed', 'en-US', 'You do not currently meet the requirements for commenting on this content.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.loginRequired', 'zh-CN', '请先登录再发表评论。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.loginRequired', 'en-US', 'Please sign in before posting a comment.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.csrfRequired', 'zh-CN', '页面已过期，请刷新后重试。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.csrfRequired', 'en-US', 'This page has expired - please refresh and try again.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.unavailable', 'zh-CN', '评论功能尚未接入。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.unavailable', 'en-US', 'Comments are not wired up yet.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.nothingSelected', 'zh-CN', '没有选择任何评论。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.nothingSelected', 'en-US', 'No comments were selected.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.parentInvalid', 'zh-CN', '回复目标不存在或不属于这条内容。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.parentInvalid', 'en-US', 'The comment being replied to does not exist or belongs to different content.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.internal', 'zh-CN', '操作失败，请稍后重试（细节只进日志）。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.err.internal', 'en-US', 'The operation failed - please try again later (details are in the logs).', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.msg.submitPending', 'zh-CN', '评论已提交，待审核。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.msg.submitPending', 'en-US', 'Your comment was submitted and will appear once it is reviewed.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.msg.reviewApproved', 'zh-CN', '已通过选中的评论。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.msg.reviewApproved', 'en-US', 'The selected comments were approved.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.msg.reviewRejected', 'zh-CN', '已驳回选中的评论。', 200, 'comment', 'internal/module/comment/enums', 1, now(), now()),
('comment.msg.reviewRejected', 'en-US', 'The selected comments were rejected.', 200, 'comment', 'internal/module/comment/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
