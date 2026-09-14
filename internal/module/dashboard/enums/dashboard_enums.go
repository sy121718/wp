// Package enums 占位说明：dashboard 为纯页面入口模块，当前无业务错误消息，
// 保留 enums 目录以符合模块结构约定，后续页面补充交互逻辑时再扩展。
package enums

// MsgDashboardTitle 仪表盘页面标题。
const MsgDashboardTitle = "MsgDashboardTitle" // 仪表盘

// MsgPagesTitle 页面列表页标题。
const MsgPagesTitle = "MsgPagesTitle" // 页面管理

// MsgArticlesTitle 文章列表页标题（I18N-005）。
const MsgArticlesTitle = "MsgArticlesTitle" // 文章

// MsgArticlesEditTitle 文章编辑页标题（I18N-005）。
const MsgArticlesEditTitle = "MsgArticlesEditTitle" // 编辑文章

// MsgProductsTitle 商品列表页标题（I18N-005）。
const MsgProductsTitle = "MsgProductsTitle" // 商品

// MsgSiteLangURLOffWarning 多语言启用但 url_mode=off 时的后台提示（I18N-016）。
const MsgSiteLangURLOffWarning = "MsgSiteLangURLOffWarning" // 当前语言 URL 方案为 off：各语言映射到同一路径，语言切换器不会渲染；如需多语言独立 URL，请将 i18n.site_lang_url_mode 设为 default_plain 或 all_prefix

// MsgContentTemplatesTitle 内容模板列表页标题（EDT-001）。
const MsgContentTemplatesTitle = "MsgContentTemplatesTitle" // 内容模板

// MsgThemesTitle 主题管理页标题。
const MsgThemesTitle = "MsgThemesTitle" // 主题管理

// MsgThemeSettingsTitle 单主题设置页标题。
const MsgThemeSettingsTitle = "MsgThemeSettingsTitle" // 主题设置

// MsgBlocksTitle 全局块管理页标题。
const MsgBlocksTitle = "MsgBlocksTitle" // 全局块

// MsgSiteSettingsTitle 站点设置页标题。
const MsgSiteSettingsTitle = "MsgSiteSettingsTitle" // 站点设置

// MsgNavigationsTitle 前台导航菜单管理页标题。
const MsgNavigationsTitle = "MsgNavigationsTitle" // 导航菜单

// MsgSiteSettingsSaved 站点设置保存成功提示。
const MsgSiteSettingsSaved = "MsgSiteSettingsSaved" // 站点设置已保存

// MsgSiteLocalesInvalid 语言清单校验失败提示（至少一种语言 / 至多一个默认且默认必须启用 / 语言码白名单）。
const MsgSiteLocalesInvalid = "MsgSiteLocalesInvalid" // 语言清单不合法：至少保留一种语言；至多一个默认语言且必须启用；语言码只能包含字母、数字与连字符

// MsgPageTranslationsTitle 翻译工作台页面标题（多语言 P5c）。
const MsgPageTranslationsTitle = "MsgPageTranslationsTitle" // 多语言

// MsgTranslationSaved 译文保存成功提示（含本次真正写入的条数）。
const MsgTranslationSaved = "MsgTranslationSaved" // 译文已保存

// MsgTranslationSaveFailed 译文写入失败统一提示（详情只记日志）。
const MsgTranslationSaveFailed = "MsgTranslationSaveFailed" // 译文保存失败，请稍后重试

// MsgTranslationInvalid 工作台提交数据不完整（表单被裁剪/篡改）。
const MsgTranslationInvalid = "MsgTranslationInvalid" // 提交数据不完整，请刷新页面后重试

// MsgTranslationStale 原文指纹不一致：页面草稿已变，需刷新后重填。
const MsgTranslationStale = "MsgTranslationStale" // 原文已变更，请刷新页面后重新翻译

// MsgTranslationLangInvalid 目标语言不属于站点启用语言。
const MsgTranslationLangInvalid = "MsgTranslationLangInvalid" // 目标语言未启用，请先在站点设置里启用

// MsgTranslationDocInvalid 页面草稿无法解析，无法列出可翻译文本。
const MsgTranslationDocInvalid = "MsgTranslationDocInvalid" // 页面草稿无法解析，请先在工作台修复页面

// MsgTranslationSiteScanSkipped 全站统计不可用（扫描失败），工作台退化为本页维度。
const MsgTranslationSiteScanSkipped = "MsgTranslationSiteScanSkipped" // 全站统计暂不可用，当前仅显示本页维度

// MsgTranslationSiteScanTooMany 全站页面数超过扫描上限，跳过全站统计。
const MsgTranslationSiteScanTooMany = "MsgTranslationSiteScanTooMany" // 页面数超过全站扫描上限，当前仅显示本页维度

// MsgInventorySourcesTitle 货源管理页标题（issue #17）。
const MsgInventorySourcesTitle = "MsgInventorySourcesTitle" // 货源管理

// MsgInventoryPurchasesTitle 采购入库页标题（issue #18）。
const MsgInventoryPurchasesTitle = "MsgInventoryPurchasesTitle" // 采购入库

// MsgMasterDataChangesTitle 主数据变更记录页标题（issue #19）。
const MsgMasterDataChangesTitle = "MsgMasterDataChangesTitle" // 变更记录

// MsgMasterDataFilterInvalid 变更记录页筛选参数无法解析（按实体查询时实体 id 不是 uuid 等）。
const MsgMasterDataFilterInvalid = "MsgMasterDataFilterInvalid" // 筛选条件无法解析，已忽略筛选

// MsgInternalError 页面 handler 内部错误统一提示（禁止直出 err.Error() 泄露内部细节）。
const MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试

// MsgCompileFailed 预览编译失败统一提示（编译器内部错误不外泄，仅提示用户检查配置）。
const MsgCompileFailed = "MsgCompileFailed" // 预览编译失败

// MsgThemeSettingsInvalid 主题设置校验失败统一提示（非法 CSS 值等，详情只记日志）。
const MsgThemeSettingsInvalid = "MsgThemeSettingsInvalid" // 主题设置不合法

// --- admin 六领域管理页标题 ---

const (
	// MsgAdministratorsTitle 管理员列表页标题。
	MsgAdministratorsTitle = "MsgAdministratorsTitle" // 管理员
	// MsgRolesTitle 角色管理页标题。
	MsgRolesTitle = "MsgRolesTitle" // 角色管理
	// MsgMenusTitle 菜单管理页标题。
	MsgMenusTitle = "MsgMenusTitle" // 菜单管理
	// MsgPermissionsTitle 权限资源页标题。
	MsgPermissionsTitle = "MsgPermissionsTitle" // 权限资源
	// MsgDepartmentsTitle 部门管理页标题。
	MsgDepartmentsTitle = "MsgDepartmentsTitle" // 部门管理
	// MsgDatarulesTitle 数据权限页标题。
	MsgDatarulesTitle = "MsgDatarulesTitle" // 数据权限
	// MsgFieldRequired 必填字段缺失统一提示。
	MsgFieldRequired = "MsgFieldRequired" // 必填字段不能为空
	// MsgAdminGenericFailed 管理页写操作失败统一提示；
	// 具体业务错误（如重名/超管保护）由对应 service 以模块 enums 返回，此处不再透出原始错误。
	MsgAdminGenericFailed = "MsgAdminGenericFailed" // 操作失败，请检查输入或联系管理员
)
