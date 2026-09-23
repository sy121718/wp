-- ========================================
-- 408 — 三个后台页面「裸 key 失败出口」收口的新增词条（masterdata / contenttemplate / analytics）
--
-- 背景：三个域各有一处 GET 页面在**工程列表装载失败**时直接 `c.String(500, shell.MsgInternalError)`：
--
--   internal/module/masterdata/inbound/http/masterdata_change_page_handle.go
--   internal/module/contenttemplate/inbound/http/content_template_handle.go
--   internal/module/analytics/inbound/http/analytics_page_handle.go
--
-- 两个问题叠在一起：
--   · **裸 key**：`c.String` 不经过任何 translate，页面上直接显示 `MsgInternalError` 这串英文；
--   · **脱页壳**：浏览器里没有页面，只有一块纯文本 —— 侧栏、页头、筛选栏全部消失，
--     用户既改不了筛选也去不了别的菜单。
--
-- 本批把三处改成**降级渲染**（空列表 + 归口提示 + HTTP 200 + 页面结构完好），
-- 并让模板能区分「空数据」与「装载失败」—— 装载失败时若仍显示「还没有站点工程」
-- 「还没有内容模板」「没有符合条件的变更记录」，是在把人往错的方向支（去建工程 / 建模板），
-- 而真正的问题是这一次没读出来。判据在 handler 算好（LoadFailed），模板只读一个布尔。
--
-- 本批新增 8 个 key × 2 语言 = 16 行：
--   admin.masterdata.loadFailed.lead      —— 装载失败时的提示条前缀（原先写死「部分数据未取到：」）
--   admin.masterdata.loadFailed.title     —— 记录 / 实体两个面板的空态标题（装载失败版）
--   admin.masterdata.loadFailed.desc      —— 同上，说明句
--   admin.analytics.no_project.loadFailed —— 统计页工程空态（装载失败版）
--   admin.content.templates.loadFailed.title —— 内容模板空态标题（装载失败版）
--   admin.content.templates.loadFailed    —— 同上，说明句
--   admin.content.templates.impact.loadFailed —— 装载失败时的引用面提示（与「能力未装配」分开：
--                                            归因不同、处置也不同 —— 前者刷新即可，后者要人工确认）
--   ErrAnalyticsInternal                  —— analytics 模块的**归口文案**（enums 里早有这个 key，
--                                            但从未登记词条；页面出口经 shell.TranslateFor 取词，
--                                            没有词条时页面上会回落中文兜底）
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**不修改任何既有词条的值**
-- （本批 7 个 key 全部此前不存在，改既有值会让「新库 / 老库」出现不可见的差异）。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.masterdata.loadFailed.lead', 'zh-CN', '页面数据未取到：', 200, 'admin', 'admin/masterdata_changes.html: 工程列表装载失败时的提示条前缀（区别于「部分数据未取到」）', 1, now(), now()),
('admin.masterdata.loadFailed.lead', 'en-US', 'Page data could not be loaded: ', 200, 'admin', 'admin/masterdata_changes.html: 工程列表装载失败时的提示条前缀（区别于「部分数据未取到」）', 1, now(), now()),
('admin.masterdata.loadFailed.title', 'zh-CN', '列表没读出来', 200, 'admin', 'admin/masterdata_changes.html: 装载失败时的空态标题（不是「没有记录」）', 1, now(), now()),
('admin.masterdata.loadFailed.title', 'en-US', 'The list could not be loaded', 200, 'admin', 'admin/masterdata_changes.html: 装载失败时的空态标题（不是「没有记录」）', 1, now(), now()),
('admin.masterdata.loadFailed.desc', 'zh-CN', '这一次没取到变更记录（不是「没有记录」）—— 先看上方的提示，刷新后重试。', 200, 'admin', 'admin/masterdata_changes.html: 装载失败时的空态说明', 1, now(), now()),
('admin.masterdata.loadFailed.desc', 'en-US', 'This request could not fetch the change records (this is not "no records") — check the notice above and refresh to retry.', 200, 'admin', 'admin/masterdata_changes.html: 装载失败时的空态说明', 1, now(), now()),
('admin.analytics.no_project.loadFailed', 'zh-CN', '工程列表没读出来 —— 不是「还没有工程」，先看上方的提示，刷新后重试。', 200, 'admin', 'admin/analytics.html: 装载失败时的空态（区别于「还没有站点工程」）', 1, now(), now()),
('admin.analytics.no_project.loadFailed', 'en-US', 'The project list could not be loaded — this is not "no projects yet". Check the notice above and refresh to retry.', 200, 'admin', 'admin/analytics.html: 装载失败时的空态（区别于「还没有站点工程」）', 1, now(), now()),
('admin.content.templates.loadFailed.title', 'zh-CN', '列表没读出来', 200, 'admin', 'admin/content_templates.html: 装载失败时的空态标题（不是「还没有内容模板」）', 1, now(), now()),
('admin.content.templates.loadFailed.title', 'en-US', 'The list could not be loaded', 200, 'admin', 'admin/content_templates.html: 装载失败时的空态标题（不是「还没有内容模板」）', 1, now(), now()),
('admin.content.templates.loadFailed', 'zh-CN', '这一次没取到模板清单（不是「还没有模板」）—— 先看上方的提示，刷新后重试。', 200, 'admin', 'admin/content_templates.html: 装载失败时的空态说明', 1, now(), now()),
('admin.content.templates.loadFailed', 'en-US', 'This request could not fetch the template list (this is not "no templates yet") — check the notice above and refresh to retry.', 200, 'admin', 'admin/content_templates.html: 装载失败时的空态说明', 1, now(), now()),
('admin.content.templates.impact.loadFailed', 'zh-CN', '本次没取到引用面（工程列表未读到），删除前请人工确认。', 200, 'admin', 'contenttemplate/inbound/http: 装载失败时的引用面提示（区别于「引用反查能力未装配」）', 1, now(), now()),
('admin.content.templates.impact.loadFailed', 'en-US', 'This request could not read the reference information (the project list failed to load); confirm manually before deleting.', 200, 'admin', 'contenttemplate/inbound/http: 装载失败时的引用面提示（区别于「引用反查能力未装配」）', 1, now(), now()),
('ErrAnalyticsInternal', 'zh-CN', '统计服务内部错误，请稍后重试', 500, 'analytics', 'analytics/inbound/http: 统计页面 / JSON 出口的归口文案（enums 里早有 key，本批首次登记词条）', 1, now(), now()),
('ErrAnalyticsInternal', 'en-US', 'Analytics service internal error, please try again later', 500, 'analytics', 'analytics/inbound/http: 统计页面 / JSON 出口的归口文案（enums 里早有 key，本批首次登记词条）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
