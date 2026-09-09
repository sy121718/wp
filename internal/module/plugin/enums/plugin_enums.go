// Package pluginenums 插件模块响应消息（未接 i18n 前中文常量）。
package pluginenums

// 响应消息。
const (
	MsgInstallSuccess   = "MsgInstallSuccess"   // 插件安装成功
	MsgInstallFailed    = "MsgInstallFailed"    // 插件安装失败
	MsgListSuccess      = "MsgListSuccess"      // 插件列表获取成功
	MsgToggleSuccess    = "MsgToggleSuccess"    // 插件状态更新成功
	MsgUninstallSuccess = "MsgUninstallSuccess" // 插件已卸载
	MsgDetailSuccess    = "MsgDetailSuccess"    // 插件详情获取成功

	ErrInvalidParam    = "ErrInvalidParam"    // 参数错误
	ErrPluginNotFound  = "ErrPluginNotFound"  // 插件不存在
	ErrInstallParse    = "ErrInstallParse"    // 插件包解析失败
	ErrUnsafePackage   = "ErrUnsafePackage"   // 插件包包含不安全内容，已拒绝
	ErrVersionRegress  = "ErrVersionRegress"  // 插件版本不能回退
	ErrStorageFailure  = "ErrStorageFailure"  // 插件包存储失败
	ErrToggleFailed    = "ErrToggleFailed"    // 插件状态更新失败
	ErrUninstallFailed = "ErrUninstallFailed" // 插件卸载失败
	ErrMigrationFailed = "ErrMigrationFailed" // 插件数据层迁移失败
)
