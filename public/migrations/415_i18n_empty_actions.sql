-- 415 · 列表页空态的「下一步动作」词条（审计 02-L §2 P1-15 剩余页）
--
-- 背景：这些页面的空态此前只有 title（有时还有 desc），缺第三段 .empty-actions ——
-- 用户看到「这里什么都没有」之后没有任何可点的下一步。本轮逐页判断「这一页真正的下一步是什么」：
--   · departments / menus            —— 新建（复用既有 admin.depts.action.create / admin.menus.create，不新增 key）；
--   · role_permissions               —— 权限树的内容来自菜单管理，本页分不了任何东西；
--   · seo（热门路径）                 —— 这栏只是摘要，没数据时该去更全的访问统计；
--   · analytics（按天 / 路径 / 来源 / 设备 / 语言）—— 真无流量时指向「把页面发出去」；
--   · analytics（工程列表装载失败档）—— 唯一动作是重试本次请求；
--   · mail_automation_run            —— 空时间线只可能是「任务还没跑到」，能做的就是刷新；
--   · mail_campaign（链接排行 / 收件人明细）—— 为空九成是活动还没启动，回营销页；
--   · page_redirects                 —— 列表空时长这样：改页面 URL 时勾「保留旧链接」也是一条来源；
--   · page_redirects（未选工程档）    —— 补 title（原先是只有 desc 的残形）与建工程入口；
--   · mail_marketing（联系人）        —— **分档**：有筛选时给「清空筛选」；无筛选（一个联系人都没有）
--                                      时不给按钮（下一步是同一屏下方折叠区的批量导入，重复给按钮是噪声）。
--
-- 为什么必须有一条迁移而不是只改模板：模板里的中文只是 t() 兜底，词条命中时显示的是库里的值
-- （与 317 / 399 / 400 / 413 同一手法，理由见 413 的头部注释）。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**只新增、不修改任何既有词条**，故无 UPDATE。
--   ConditionSQL 由迁移器 db.Raw 直接执行，没有参数替换，判定用的 key 只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.roles.perm.empty.action', 'zh-CN', '去菜单管理', 200, 'admin', 'admin/role_permissions.html: 权限树为空时的空态动作（去菜单管理建菜单）', 1, now(), now()),
('admin.roles.perm.empty.action', 'en-US', 'Go to menu management', 200, 'admin', 'admin/role_permissions.html: 权限树为空时的空态动作（去菜单管理建菜单）', 1, now(), now()),

('admin.seo.paths.empty.action', 'zh-CN', '去访问统计', 200, 'admin', 'admin/seo.html: 热门路径为空时的空态动作（去更全的访问统计）', 1, now(), now()),
('admin.seo.paths.empty.action', 'en-US', 'Open analytics', 200, 'admin', 'admin/seo.html: 热门路径为空时的空态动作（去更全的访问统计）', 1, now(), now()),

('admin.analytics.no_project.refresh', 'zh-CN', '刷新重试', 200, 'admin', 'admin/analytics.html: 工程列表装载失败档的空态动作（重试本次请求）', 1, now(), now()),
('admin.analytics.no_project.refresh', 'en-US', 'Refresh and retry', 200, 'admin', 'admin/analytics.html: 工程列表装载失败档的空态动作（重试本次请求）', 1, now(), now()),
('admin.analytics.empty.action', 'zh-CN', '去页面管理发布页面', 200, 'admin', 'admin/analytics.html: 统计维度为空时的空态动作（没有流量 = 还没有发布页面）', 1, now(), now()),
('admin.analytics.empty.action', 'en-US', 'Open page management to publish pages', 200, 'admin', 'admin/analytics.html: 统计维度为空时的空态动作（没有流量 = 还没有发布页面）', 1, now(), now()),

('admin.mail.automation_run.timeline.empty.action', 'zh-CN', '刷新', 200, 'admin', 'admin/mail_automation_run.html: 执行记录为空时的空态动作（任务还没跑到，刷新看进度）', 1, now(), now()),
('admin.mail.automation_run.timeline.empty.action', 'en-US', 'Refresh', 200, 'admin', 'admin/mail_automation_run.html: 执行记录为空时的空态动作（任务还没跑到，刷新看进度）', 1, now(), now()),

('admin.mail.campaign.empty.action', 'zh-CN', '回营销页启动群发', 200, 'admin', 'admin/mail_campaign.html: 链接排行与收件人明细为空时的空态动作（回营销页看活动状态 / 启动）', 1, now(), now()),
('admin.mail.campaign.empty.action', 'en-US', 'Back to marketing to start the campaign', 200, 'admin', 'admin/mail_campaign.html: 链接排行与收件人明细为空时的空态动作（回营销页看活动状态 / 启动）', 1, now(), now()),

('admin.redirect.empty.action', 'zh-CN', '去页面管理改 URL', 200, 'admin', 'admin/page_redirects.html: 重定向列表为空时的空态动作（改 URL 时勾「保留旧链接」）', 1, now(), now()),
('admin.redirect.empty.action', 'en-US', 'Open page management to change a URL', 200, 'admin', 'admin/page_redirects.html: 重定向列表为空时的空态动作（改 URL 时勾「保留旧链接」）', 1, now(), now()),
('admin.redirect.pick_project.title', 'zh-CN', '还没有可管理的站点工程', 200, 'admin', 'admin/page_redirects.html: 未选到工程档的空态标题（原先是只有 desc 的残形）', 1, now(), now()),
('admin.redirect.pick_project.title', 'en-US', 'No site project to manage yet', 200, 'admin', 'admin/page_redirects.html: 未选到工程档的空态标题（原先是只有 desc 的残形）', 1, now(), now()),
('admin.redirect.pick_project.action', 'zh-CN', '去页面管理建工程', 200, 'admin', 'admin/page_redirects.html: 未选到工程档的空态动作（重定向挂在工程下）', 1, now(), now()),
('admin.redirect.pick_project.action', 'en-US', 'Create a project in page management', 200, 'admin', 'admin/page_redirects.html: 未选到工程档的空态动作（重定向挂在工程下）', 1, now(), now()),

('admin.mail.marketing.contacts.empty.clear', 'zh-CN', '清空筛选看全部', 200, 'admin', 'admin/mail_marketing.html: 联系人空态（有筛选档）的动作', 1, now(), now()),
('admin.mail.marketing.contacts.empty.clear', 'en-US', 'Clear filters to see all', 200, 'admin', 'admin/mail_marketing.html: 联系人空态（有筛选档）的动作', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
