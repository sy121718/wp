-- ========================================
-- 403 — 项目域后台页面「裸文本失败出口」收口的新增词条
--
-- 背景：project 域的页面 handler（主题管理 / 主题设置 / 站点设置）有 20 处
--   `c.String(4xx, "中文硬编码")` 与 `c.String(500, 归口 key)` 直接写响应 ——
--   浏览器里没有页面，只有一块纯文本，侧边栏 / 页头 / 表单全部消失，用户改不了也退不回。
--
-- 本批把这些出口改成「303 回来源页 + ?err=<当前语言文案>」或「回渲染表单页 + 错误槽」，
-- 而**文案必须走 i18n**（页面提示是直接渲染的文本，不经过 response 的 translate）：
--   · 原先硬编码在 Go 里的 5 条中文长句（GA4 / GSC / 404 页 / 语言 URL 方案）此前
--     连词条都没有，是「模板 t() 兜底」的反向情况 —— 原文在代码里，译文无从谈起；
--   · 原先写成 `c.String(500, "MsgInternalError")` 的地方渲染的是**裸 key**，
--     页面上直接显示 `MsgInternalError` 这串英文。
--
-- 本批新增 8 个 key（2 语言共 16 行）+ 给既有的 MsgThemeSettingsInvalid 补 en-US
-- （它此前只有 zh-CN，英文页面上会回落中文原文）。
--
-- ErrProjectRequired 是本批**对账测试抓出来的存量缺陷**：enums 里早有这个 key
-- （project_enums.go 用它表示「未指定工程且解析不出工程作用域」），但从未登记词条，
-- 谁把它渲染到页面上都会显示裸 key。本批的 theme 列表页正好要用它（新建主题缺工程）。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**不修改任何既有词条的值**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- 面向用户的措辞（enums 注释里的「需要显式工程作用域」是给开发者的语义说明，
-- 页面提示要说「用户现在该做什么」）。
('ErrProjectRequired', 'zh-CN', '请先选择要操作的站点工程', 400, 'project', 'project/inbound/http: 页面入口没有工程作用域（存量 key，本批首次登记词条）', 1, now(), now()),
('ErrProjectRequired', 'en-US', 'Select a site project first', 400, 'project', 'project/inbound/http: 页面入口没有工程作用域（存量 key，本批首次登记词条）', 1, now(), now()),
('ErrThemeIDRequired', 'zh-CN', '缺少主题 id', 400, 'project', 'project/inbound/http: 主题设置页缺 id（页面出口走 ?err=）', 1, now(), now()),
('ErrThemeIDRequired', 'en-US', 'Missing theme id', 400, 'project', 'project/inbound/http: 主题设置页缺 id（页面出口走 ?err=）', 1, now(), now()),
('MsgThemeSettingsInvalid', 'en-US', 'Invalid theme settings', 400, 'project', 'project/inbound/http: 主题设置未通过 CSS 值白名单校验（补 en-US）', 1, now(), now()),
('MsgThemeSettingsRefreshFailed', 'zh-CN', '主题设置已保存，但整站页面刷新失败 —— 稍后在页面列表里重建即可，不必重填。', 500, 'project', 'project/inbound/http: 保存成功但整站刷新失败（部分成功提示）', 1, now(), now()),
('MsgThemeSettingsRefreshFailed', 'en-US', 'Theme settings saved, but refreshing site pages failed — rebuild them from the page list later; no need to re-enter.', 500, 'project', 'project/inbound/http: 保存成功但整站刷新失败（部分成功提示）', 1, now(), now()),
('ErrSiteSettingsNameRequired', 'zh-CN', '工程与站点名称不能为空', 400, 'project', 'project/inbound/http: 站点设置保存缺少工程或站点名', 1, now(), now()),
('ErrSiteSettingsNameRequired', 'en-US', 'Project and site name are required', 400, 'project', 'project/inbound/http: 站点设置保存缺少工程或站点名', 1, now(), now()),
('ErrGA4IDInvalid', 'zh-CN', 'GA4 测量 ID 格式不合法（形如 G-XXXXXXXXXX，只允许字母与数字）', 400, 'project', 'project/inbound/http: GA4 测量 ID 在保存时校验（与构建期注入同一判据）', 1, now(), now()),
('ErrGA4IDInvalid', 'en-US', 'Invalid GA4 measurement ID (expected G-XXXXXXXXXX, letters and digits only)', 400, 'project', 'project/inbound/http: GA4 测量 ID 在保存时校验（与构建期注入同一判据）', 1, now(), now()),
('ErrGSCVerificationInvalid', 'zh-CN', 'Search Console 验证 token 格式不合法（base64url：字母、数字、- 与 _，8~128 位）', 400, 'project', 'project/inbound/http: GSC 验证 token 在保存时校验（与构建期注入同一判据）', 1, now(), now()),
('ErrGSCVerificationInvalid', 'en-US', 'Invalid Search Console verification token (base64url: letters, digits, - and _, 8-128 chars)', 400, 'project', 'project/inbound/http: GSC 验证 token 在保存时校验（与构建期注入同一判据）', 1, now(), now()),
('ErrNotFoundHTMLTooLong', 'zh-CN', '自定义 404 页内容过长（上限 32 KiB）', 400, 'project', 'project/inbound/http: 自定义 404 页长度上限（模板 maxlength 是体验，这里是判据）', 1, now(), now()),
('ErrNotFoundHTMLTooLong', 'en-US', 'Custom 404 page content is too long (32 KiB max)', 400, 'project', 'project/inbound/http: 自定义 404 页长度上限（模板 maxlength 是体验，这里是判据）', 1, now(), now()),
('ErrLangURLModeInvalid', 'zh-CN', '语言 URL 方案取值非法（可选 off / default_plain / all_prefix）', 400, 'project', 'project/inbound/http: 语言 URL 方案在保存时校验（与构建期同一枚举）', 1, now(), now()),
('ErrLangURLModeInvalid', 'en-US', 'Invalid language URL mode (one of off / default_plain / all_prefix)', 400, 'project', 'project/inbound/http: 语言 URL 方案在保存时校验（与构建期同一枚举）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
