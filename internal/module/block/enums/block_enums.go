package blockenums

import (
	"net/http"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
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

	MsgInternalError = response.InternalKey

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

func Err(key string) error {
	err := facing.New(key)
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
