package blockenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

const (
	MsgBlockTitle = "MsgBlockTitle"

	ErrBlockParamRequired = "ErrBlockParamRequired"
	ErrBlockNotFound      = "ErrBlockNotFound"
	ErrProjectNotFound    = "ErrProjectNotFound"

	ErrBlockProjectRequired   = "ErrBlockProjectRequired"
	ErrBlockNameRequired      = "ErrBlockNameRequired"
	ErrBlockInvalidDoc        = "ErrBlockInvalidDoc"
	ErrBlockInvalidKind       = "ErrBlockInvalidKind"
	ErrBlockInvalidCategory   = "ErrBlockInvalidCategory"
	ErrBlockDuplicate         = "ErrBlockDuplicate"
	ErrBlockPresetInvalid     = "ErrBlockPresetInvalid"
	ErrBlockPresetUnavailable = "ErrBlockPresetUnavailable"

	ErrBlockVersionConflict = "ErrBlockVersionConflict"

	ErrBlockInvalidReuseMode = "ErrBlockInvalidReuseMode"
	ErrBlockInUse            = "ErrBlockInUse"

	MsgBlockCloned = "MsgBlockCloned"

	MsgBlockCreated = "MsgBlockCreated"

	MsgBlockUsagePageDocument         = "MsgBlockUsagePageDocument"
	MsgBlockUsagePageStructure        = "MsgBlockUsagePageStructure"
	MsgBlockUsagePageRevision         = "MsgBlockUsagePageRevision"
	MsgBlockUsageThemeSlot            = "MsgBlockUsageThemeSlot"
	MsgBlockUsageBlockDocument        = "MsgBlockUsageBlockDocument"
	MsgBlockUsageContentTemplate      = "MsgBlockUsageContentTemplate"
	MsgBlockUsagePresentationInstance = "MsgBlockUsagePresentationInstance"

	MsgInternalError = "MsgInternalError"

	ImpactUnavailableNoPageCapability  = "admin.blocks.impact.unavailable.noPageCapability"
	ImpactUnavailableProjectReadFailed = "admin.blocks.impact.unavailable.projectReadFailed"
	RefCountUnknown                    = "admin.blocks.refCount.unknown"
	RefCountNone                       = "admin.blocks.refCount.none"
	RefCountPages                      = "admin.blocks.refCount.pages"
	UsageMore                          = "admin.blocks.usage.more"
	ContentSavedRebuildQueued          = "admin.blocks.content.savedRebuild"

	ImpactStagePlan  = "admin.pages.impact.stage_plan"
	ImpactStageBuild = "admin.pages.impact.stage_build"
)

var errFallbacks = map[string]string{
	ErrBlockVersionConflict:   "该全局块已被其他人修改（当前第 %s 版），请刷新后重试",
	ErrBlockParamRequired:     "块参数不能为空",
	ErrBlockNotFound:          "全局块不存在",
	ErrProjectNotFound:        "工程不存在",
	ErrBlockProjectRequired:   "缺少可作用域的站点工程，无法定位全局块的工程归属",
	ErrBlockNameRequired:      "块名称不能为空",
	ErrBlockInvalidDoc:        "块文档不合法",
	ErrBlockInvalidKind:       "块类型不合法",
	ErrBlockInvalidCategory:   "块分类不合法",
	ErrBlockDuplicate:         "同名块已存在",
	ErrBlockPresetInvalid:     "预设标识、版本或文档组合不合法",
	ErrBlockPresetUnavailable: "所选预设或版本不可用",
	ErrBlockInvalidReuseMode:  "块复用方式不合法（仅支持 global/template）",
	ErrBlockInUse:             "全局块已被页面或主题引用，无法删除或切换为一次性复制；可强制删除（引用页面将退化为无该块）",
}

func Err(key string) error {
	err := facing.New(key, errFallbacks[key])
	switch key {
	case ErrBlockNotFound, ErrProjectNotFound:
		return facing.WithStatus(err, http.StatusNotFound)
	case ErrBlockDuplicate, ErrBlockInUse, ErrBlockVersionConflict:
		return facing.WithStatus(err, http.StatusConflict)
	case ErrBlockPresetInvalid, ErrBlockPresetUnavailable:
		return facing.WithStatus(err, http.StatusBadRequest)
	}
	return err
}

func FallbackFor(key string) string { return errFallbacks[key] }
