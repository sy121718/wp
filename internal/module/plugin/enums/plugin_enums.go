package pluginenums

import "go_wp/pkg/facing"

const (
	MsgInstallSuccess   = "MsgInstallSuccess"
	MsgInstallFailed    = "MsgInstallFailed"
	MsgListSuccess      = "MsgListSuccess"
	MsgToggleSuccess    = "MsgToggleSuccess"
	MsgUninstallSuccess = "MsgUninstallSuccess"
	MsgDetailSuccess    = "MsgDetailSuccess"

	ErrInvalidParam    = "ErrInvalidParam"
	ErrPluginNotFound  = "ErrPluginNotFound"
	ErrInstallParse    = "ErrInstallParse"
	ErrUnsafePackage   = "ErrUnsafePackage"
	ErrVersionRegress  = "ErrVersionRegress"
	ErrStorageFailure  = "ErrStorageFailure"
	ErrToggleFailed    = "ErrToggleFailed"
	ErrUninstallFailed = "ErrUninstallFailed"
	ErrMigrationFailed = "ErrMigrationFailed"
)

const (
	ErrModuleUnwired = "plugin.err.moduleUnwired"

	ErrInstallNoFile = "plugin.err.installNoFile"

	ErrPackageUnreadable = "plugin.err.packageUnreadable"

	ErrListFailed = "plugin.err.listFailed"

	NoticeInstallReselect = "plugin.notice.installReselect"

	TitlePlugins = "plugin.title.plugins"

	HintComponentDefault = "plugin.hint.componentDefault"
)

var errFallbacks = map[string]string{
	ErrModuleUnwired:     "插件模块未装配，该操作无法执行",
	ErrInstallNoFile:     "没有收到插件包文件",
	ErrPackageUnreadable: "插件包读取失败，或文件超过 52MB 上限",
	ErrListFailed:        "插件列表加载失败",
	ErrInvalidParam:      "请求参数无效",
	ErrPluginNotFound:    "插件不存在",
	ErrInstallParse:      "插件包解析失败",
	ErrUnsafePackage:     "插件包包含不安全内容，已拒绝",
	ErrVersionRegress:    "插件版本不能回退",
	ErrStorageFailure:    "插件包存储失败",
	ErrToggleFailed:      "插件状态更新失败",
	ErrUninstallFailed:   "插件卸载失败",
	ErrMigrationFailed:   "插件数据层迁移失败",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
