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
// ---- 主题包资产端口错误（主题包导入导出已移除，端口保留给装配层注入的适配器） ----
// 值一律等于常量名：文案真源在 sys_i18n（迁移 058 的既有口径），此处只做键。
)

const (
	MsgProjectCreated = "MsgProjectCreated" // 站点工程创建成功
	MsgProjectUpdated = "MsgProjectUpdated" // 站点工程更新成功
)
