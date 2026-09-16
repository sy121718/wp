// Package projectenums 统一管理 project 模块响应消息。
package projectenums

const (
	ErrProjectNotFound = "ErrProjectNotFound" // 站点工程不存在
	ErrInvalidSettings = "ErrInvalidSettings" // 站点设置必须是合法的 JSON 对象
	ErrInvalidName     = "ErrInvalidName"     // 站点工程名称不能为空且不能超过 200 个字符
	// ErrInvalidParam 请求本身不合法（nil 请求等），与名称/设置/工程存在性无关。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrProjectInternal 工程服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrProjectInternal = "ErrProjectInternal" // 工程服务内部错误
	// ErrProjectRequired 未指定工程且解析不出工程作用域（0 个工程，或入口本身没有工程参数）。
	//
	// DB-009 第三批：主题入口（GetTheme/UpdateTheme/ActivateTheme/DeleteTheme）的签名里
	// 没有工程参数（后台页面直接依赖该契约），此前靠「工程表恰好一个工程」猜作用域，
	// 多工程部署下退化为「不限工程」—— 换非超级角色后那是静默「主题不存在」。
	// 现在改为逐工程扇出定位；只有连工程表都读不出内容时才用这个错误显式失败。
	ErrProjectRequired = "ErrProjectRequired" // 需要显式工程作用域
)

const (
	// ---- 站点主题（Theme）业务错误文案，project 模块下 theme 能力 ----
	ErrThemeNameRequired    = "ErrThemeNameRequired"    // 主题名称不能为空
	ErrThemeNotFound        = "ErrThemeNotFound"        // 主题不存在
	ErrThemeIsActive        = "ErrThemeIsActive"        // 激活主题不可删除，请先切换到其他主题
	ErrThemeDuplicateName   = "ErrThemeDuplicateName"   // 同名主题已存在
	ErrThemeProjectIDEmpty  = "ErrThemeProjectIDEmpty"  // 工程 ID 不能为空
	ErrInvalidThemeSettings = "ErrInvalidThemeSettings" // 无效的主题设置
	// ErrThemeInternal 主题服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrThemeInternal = "ErrThemeInternal" // 主题服务内部错误
)

const (
	// ---- 主题包（Theme Bundle）导入导出业务错误文案，project 模块下 theme 能力 ----
	// 值一律等于常量名：文案真源在 sys_i18n（迁移 058 的既有口径），此处只做键。
	ErrThemeBundleFileRequired    = "ErrThemeBundleFileRequired"    // 未提供主题包文件
	ErrThemeBundleFormatUnknown   = "ErrThemeBundleFormatUnknown"   // 不是有效的主题包（format 标识不符）
	ErrThemeBundleMissingManifest = "ErrThemeBundleMissingManifest" // 主题包缺少 manifest.json
	ErrThemeBundleManifestInvalid = "ErrThemeBundleManifestInvalid" // 主题包 manifest 结构非法
	ErrThemeBundleVersionTooNew   = "ErrThemeBundleVersionTooNew"   // 主题包版本号高于当前支持的最高版本
	ErrThemeBundleVersionInvalid  = "ErrThemeBundleVersionInvalid"  // 主题包版本号非法（非正整数）
	ErrThemeBundleUnsafeEntry     = "ErrThemeBundleUnsafeEntry"     // 主题包含不安全条目（路径穿越 / 未知扩展名）
	ErrThemeBundleTooLarge        = "ErrThemeBundleTooLarge"        // 主题包超过大小或条目数上限
	ErrThemeBundleTokensInvalid   = "ErrThemeBundleTokensInvalid"   // 主题令牌非法（非对象或含非法 CSS 值）
	ErrThemeBundleBlockMissing    = "ErrThemeBundleBlockMissing"    // 主题包声明引用的块不在包内
	ErrThemeBundleBlockCycle      = "ErrThemeBundleBlockCycle"      // 主题包内块引用成环（导入顺序未定义）
	ErrThemeBundleAssetMissing    = "ErrThemeBundleAssetMissing"    // 主题包引用的资产不存在
	ErrThemeBundlePortUnavailable = "ErrThemeBundlePortUnavailable" // 主题包资产端口未装配（装配期注入缺失）
)

const (
	MsgProjectCreated = "MsgProjectCreated" // 站点工程创建成功
	MsgProjectUpdated = "MsgProjectUpdated" // 站点工程更新成功
	// MsgThemeBundleImported 主题包导入成功（媒体依赖全部满足）。
	MsgThemeBundleImported = "MsgThemeBundleImported"
	// MsgThemeBundleImportedPartial 主题包导入成功，但存在缺失媒体（清单随响应返回）。
	MsgThemeBundleImportedPartial = "MsgThemeBundleImportedPartial"
)
