-- 717 · page 域八条业务错误的词条（此前零覆盖，用户只能看到归口文案）
--
-- 背景：这八个 key 在 `internal/module/page/enums/page_enums.go` 里早已声明，
--   但 `public/migrations/` 里没有任何一条 seed —— 出口 `translate` 查不到词条就
--   `InternalText(c)`，于是「该语言已被本页排除，无需重复操作」这类可行动提示在生产
--   环境变成「系统内部错误，请稍后重试」，用户和运维都看不出哪一步错了。
--
-- 为什么 key 是裸常量名（ErrPageLangExcluded 而不是 page.err.langExcluded）：
--   裸常量名当 key 是全仓既有约定（admin / analytics / block / build / artifact /
--   blueprint 等 20+ 个域一致），`internal/adminui/page_error_test.go` 明确把
--   "ErrInvalidParam" 当合法业务 key 形态在测。本文件沿用同一形状，不在这里插异类。
--   本文件里点分形式只有 ErrActionTargetInUse（它本来就已 seed，不在本批）。
--
-- 文案口径：语言类四条的中文取自 `internal/module/page/inbound/http/page_lang_err_facing_test.go`
--   的 cases（那里有成对的 key + 中文，是这四个 key 唯一的成品句来源）；另四条按键的
--   触发上下文按**用户看到的语义**写：
--     · ErrRevisionNotFound  —— ListRevisions 找不到指定版本（pages_handle.go:695）
--     · ErrInputMoved        —— 发布 / 改 URL 期间捕获的输入代次已过期（page_publish.go:298 / :1220）
--     · ErrRebuildOverloaded —— 失效重建超出单次上限且构建队列未接入（page_dependency.go:169 / page_publish.go:2758）
--     · ErrRedirectConflict  —— 目标路径的归属已不是重定向行（page_redirect.go:192 / :219）
--
-- http_code 与出口一致：pages_handle.go 的 pageErrorStatus 把四个 *Lang* 与 ErrInputMoved
--   都映射到 409；ErrRevisionNotFound 走显式 404；ErrRebuildOverloaded 是 503；
--   ErrRedirectConflict 是 409。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见 register_page_missing_error_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrPageLangExcluded', 'zh-CN', '该语言已被本页排除', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrPageLangExcluded（page_lang_err_facing_test.go cases）', 1, now(), now()),
('ErrPageLangExcluded', 'en-US', 'This language is already excluded from this page.', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrPageLangExcluded', 1, now(), now()),

('ErrCannotExcludeDefaultLang', 'zh-CN', '不能排除站点默认语言：它的产物承载 x-default，且「默认语言无前缀」的路径映射以它为锚点', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrCannotExcludeDefaultLang（page_lang_err_facing_test.go cases）', 1, now(), now()),
('ErrCannotExcludeDefaultLang', 'en-US', 'The site default language cannot be excluded: its artifact carries x-default, and the "default language has no prefix" path mapping is anchored on it.', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrCannotExcludeDefaultLang', 1, now(), now()),

('ErrLangAlreadyExcluded', 'zh-CN', '该语言已被本页排除，无需重复操作', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrLangAlreadyExcluded（page_lang_err_facing_test.go cases）', 1, now(), now()),
('ErrLangAlreadyExcluded', 'en-US', 'This language is already excluded from this page; no need to repeat the operation.', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrLangAlreadyExcluded', 1, now(), now()),

('ErrLangNotExcluded', 'zh-CN', '该语言未被本页排除，无从恢复', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrLangNotExcluded（page_lang_err_facing_test.go cases）', 1, now(), now()),
('ErrLangNotExcluded', 'en-US', 'This language is not excluded from this page, so there is nothing to restore.', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrLangNotExcluded', 1, now(), now()),

('ErrRevisionNotFound', 'zh-CN', '该历史版本不存在，可能已被其他管理员清理，请刷新页面后重试', 404, 'page', 'internal/module/page/enums/page_enums.go: ErrRevisionNotFound（pages_handle.go:695 ListRevisions）', 1, now(), now()),
('ErrRevisionNotFound', 'en-US', 'That revision does not exist. Another administrator may have cleaned it up; refresh the page and try again.', 404, 'page', 'internal/module/page/enums/page_enums.go: ErrRevisionNotFound (pages_handle.go:695 ListRevisions)', 1, now(), now()),

('ErrInputMoved', 'zh-CN', '页面内容或依赖在你操作期间发生了变更，本次变更已放弃，请刷新后重试', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrInputMoved（page_publish.go:298 / :1220，捕获的输入代次已过期）', 1, now(), now()),
('ErrInputMoved', 'en-US', 'The page content or its dependencies changed while you were working, so this change was abandoned. Refresh and try again.', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrInputMoved (page_publish.go:298 / :1220, captured input generation went stale)', 1, now(), now()),

('ErrRebuildOverloaded', 'zh-CN', '需要重建的页面太多，超出单次处理上限，超出的页面已留待下次重建，请稍后再试', 503, 'page', 'internal/module/page/enums/page_enums.go: ErrRebuildOverloaded（page_dependency.go:169 / page_publish.go:2758，构建队列未接入）', 1, now(), now()),
('ErrRebuildOverloaded', 'en-US', 'Too many pages need rebuilding for a single pass. The remaining pages are left for the next rebuild; try again later.', 503, 'page', 'internal/module/page/enums/page_enums.go: ErrRebuildOverloaded (page_dependency.go:169 / page_publish.go:2758, build queue unavailable)', 1, now(), now()),

('ErrRedirectConflict', 'zh-CN', '该路径的归属已经变化，不再是本页的重定向，请刷新后重新操作', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrRedirectConflict（page_redirect.go:192 / :219，路径归属已非重定向行）', 1, now(), now()),
('ErrRedirectConflict', 'en-US', 'That path is no longer a redirect owned by this page. Refresh and try again.', 409, 'page', 'internal/module/page/enums/page_enums.go: ErrRedirectConflict (page_redirect.go:192 / :219, route owner is no longer a redirect)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
