// Package projectenums 统一管理 project 模块响应消息。
package projectenums

const (
	ErrProjectNotFound = "站点工程不存在"
	ErrInvalidSettings = "站点设置必须是合法的 JSON 对象"
	ErrInvalidName     = "站点工程名称不能为空且不能超过 200 个字符"
	// ErrInvalidParam 请求本身不合法（nil 请求等），与名称/设置/工程存在性无关。
	ErrInvalidParam = "请求参数无效"
	// ErrProjectInternal 工程服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrProjectInternal = "工程服务内部错误"
)

const (
	// ---- 站点主题（Theme）业务错误文案，project 模块下 theme 能力 ----
	ErrThemeNameRequired    = "主题名称不能为空"
	ErrThemeNotFound        = "主题不存在"
	ErrThemeIsActive        = "激活主题不可删除，请先切换到其他主题"
	ErrThemeDuplicateName   = "同名主题已存在"
	ErrThemeProjectIDEmpty  = "工程 ID 不能为空"
	ErrInvalidThemeSettings = "无效的主题设置"
	// ErrThemeInternal 主题服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrThemeInternal = "主题服务内部错误"
)

const (
	MsgProjectCreated = "站点工程创建成功"
	MsgProjectUpdated = "站点工程更新成功"
)
