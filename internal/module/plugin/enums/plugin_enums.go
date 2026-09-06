// Package pluginenums 插件模块响应消息（未接 i18n 前中文常量）。
package pluginenums

// 响应消息。
const (
	MsgInstallSuccess   = "插件安装成功"
	MsgInstallFailed    = "插件安装失败"
	MsgListSuccess      = "插件列表获取成功"
	MsgToggleSuccess    = "插件状态更新成功"
	MsgUninstallSuccess = "插件已卸载"
	MsgDetailSuccess    = "插件详情获取成功"

	ErrInvalidParam    = "参数错误"
	ErrPluginNotFound  = "插件不存在"
	ErrInstallParse    = "插件包解析失败"
	ErrUnsafePackage   = "插件包包含不安全内容，已拒绝"
	ErrVersionRegress  = "插件版本不能回退"
	ErrStorageFailure  = "插件包存储失败"
	ErrToggleFailed    = "插件状态更新失败"
	ErrUninstallFailed = "插件卸载失败"
)
