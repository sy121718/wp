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

func Err(key string) error { return facing.New(key) }

