-- ========================================
-- 221 · 主题包（Theme Bundle）业务文案词条（审计 VIS-014）
--
-- enums 的值是 i18n key（internal/module/project/enums/project_enums.go 的
-- ErrThemeBundle* / MsgThemeBundleImported*），响应层 translate 未命中时会原样返回 key，
-- 所以每个 key 都必须有落处。zh-CN / en-US 各一条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源
--（DO UPDATE 会让运营改过的词条在下次部署时静默回滚）。
-- 注意：VALUES 列表最后一项末尾不能带逗号（整段交给 db.Exec，不做语句切分）。
-- 注册：public/migrations/register_admin_i18n.go（Seed 221-i18n-seed-theme-bundle）。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrThemeBundleFileRequired', 'zh-CN', '未提供主题包文件', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleFileRequired', 'en-US', 'Theme bundle file is required', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleFormatUnknown', 'zh-CN', '不是有效的主题包（缺少 go-wp.theme-bundle 标识）', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleFormatUnknown', 'en-US', 'Not a valid theme bundle (missing the go-wp.theme-bundle marker)', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleMissingManifest', 'zh-CN', '主题包缺少 manifest.json', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleMissingManifest', 'en-US', 'Theme bundle is missing manifest.json', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleManifestInvalid', 'zh-CN', '主题包 manifest 结构非法', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleManifestInvalid', 'en-US', 'Theme bundle manifest is malformed', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleVersionTooNew', 'zh-CN', '主题包格式版本高于当前支持的最高版本', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleVersionTooNew', 'en-US', 'Theme bundle format version is newer than this build supports', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleVersionInvalid', 'zh-CN', '主题包格式版本号非法', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleVersionInvalid', 'en-US', 'Theme bundle format version is invalid', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleUnsafeEntry', 'zh-CN', '主题包含不安全条目（路径穿越或未知扩展名）', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleUnsafeEntry', 'en-US', 'Theme bundle contains an unsafe entry (path traversal or unknown extension)', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleTooLarge', 'zh-CN', '主题包超过大小或条目数上限', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleTooLarge', 'en-US', 'Theme bundle exceeds the size or entry-count limit', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleTokensInvalid', 'zh-CN', '主题令牌非法（非 JSON 对象或含非法 CSS 值）', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleTokensInvalid', 'en-US', 'Theme tokens are invalid (not a JSON object, or contain an unsafe CSS value)', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleBlockMissing', 'zh-CN', '主题包声明引用的块不在包内', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleBlockMissing', 'en-US', 'A block declared by the bundle is missing from the archive', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleBlockCycle', 'zh-CN', '主题包内块引用成环，导入顺序未定义', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleBlockCycle', 'en-US', 'Block references in the bundle form a cycle, so the import order is undefined', 400, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleAssetMissing', 'zh-CN', '主题包引用的资产不存在', 404, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundleAssetMissing', 'en-US', 'An asset referenced by the bundle does not exist', 404, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundlePortUnavailable', 'zh-CN', '主题包资产服务未就绪（装配期端口未注入）', 503, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('ErrThemeBundlePortUnavailable', 'en-US', 'Theme bundle asset service is not available (port not wired at assembly time)', 503, 'error', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('MsgThemeBundleImported', 'zh-CN', '主题包导入成功', 200, 'msg', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('MsgThemeBundleImported', 'en-US', 'Theme bundle imported', 200, 'msg', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('MsgThemeBundleImportedPartial', 'zh-CN', '主题包导入成功，但有媒体文件缺失，请查看响应中的缺失清单', 200, 'msg', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW()),
('MsgThemeBundleImportedPartial', 'en-US', 'Theme bundle imported with missing media files; see the missing list in the response', 200, 'msg', 'internal/module/project/enums/project_enums.go', 1, NOW(), NOW())
ON CONFLICT (item_key, lang) DO NOTHING;
