package pageenums

import (
	"strings"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

const (
	ErrActionTargetInUse = "page.err.actionTargetInUse"

	ErrPageLangExcluded = "ErrPageLangExcluded"

	ErrCannotExcludeDefaultLang = "ErrCannotExcludeDefaultLang"

	ErrLangAlreadyExcluded = "ErrLangAlreadyExcluded"

	ErrLangNotExcluded = "ErrLangNotExcluded"
	ErrInvalidParam    = "ErrInvalidParam"
	ErrPageNotFound    = "ErrPageNotFound"
	ErrProjectNotFound = "ErrProjectNotFound"

	ErrProjectRequired      = "ErrProjectRequired"
	ErrInvalidKind          = "ErrInvalidKind"
	ErrInvalidDocument      = "ErrInvalidDocument"
	ErrInvalidPath          = "ErrInvalidPath"
	ErrDraftVersionConflict = "ErrDraftVersionConflict"
	ErrPathOccupied         = "ErrPathOccupied"

	ErrRevisionNotFound = "ErrRevisionNotFound"

	ErrBlueprintUnavailable = "ErrBlueprintUnavailable"

	ErrBlueprintInvalid   = "ErrBlueprintInvalid"
	ErrNoStagedArtifact   = "ErrNoStagedArtifact"
	ErrRollbackTargetMiss = "ErrRollbackTargetMiss"
	ErrRebuildRequired    = "ErrRebuildRequired"

	ErrInputMoved = "ErrInputMoved"

	ErrRebuildOverloaded = "ErrRebuildOverloaded"

	ErrInvalidSlot  = "ErrInvalidSlot"
	ErrSlotPageMiss = "ErrSlotPageMiss"

	ErrRedirectUnavailable = "ErrRedirectUnavailable"
	ErrRedirectNotFound    = "ErrRedirectNotFound"
	ErrRedirectOccupied    = "ErrRedirectOccupied"
	ErrRedirectTargetMiss  = "ErrRedirectTargetMiss"
	ErrRedirectLoop        = "ErrRedirectLoop"

	ErrRedirectConflict = "ErrRedirectConflict"

	// ErrBatchReject 是多项批量删除被整体拒绝时的公共主消息；
	// 单条请求仍给该条的具体业务原因。
	ErrBatchReject = "pageBatchReject"
)

const (
	MsgPageCreated     = "MsgPageCreated"
	MsgPageDeleted     = "MsgPageDeleted"
	MsgDraftSaved      = "MsgDraftSaved"
	MsgPageDetail      = "MsgPageDetail"
	MsgRevisionsListed = "MsgRevisionsListed"
	MsgBuildReady      = "MsgBuildReady"
	MsgPublished       = "MsgPublished"
	MsgRollbackDone    = "MsgRollbackDone"
	MsgURLUpdated      = "MsgURLUpdated"
	MsgSiteSlotBound   = "MsgSiteSlotBound"
	MsgSiteSlotUnbound = "MsgSiteSlotUnbound"
	MsgRedirectCreated = "MsgRedirectCreated"
	MsgRedirectDeleted = "MsgRedirectDeleted"
	MsgRedirectMerged  = "MsgRedirectMerged"
)

const (
	BulkPageNoneSelected = "page.bulk.pageNoneSelected"
	BulkPageAllDeleted   = "page.bulk.pageAllDeleted"
	BulkPageAllSkipped   = "page.bulk.pageAllSkipped"
	BulkPagePartial      = "page.bulk.pagePartial"

	BulkPageMissingID = "page.bulk.pageMissingID"

	PageFormProjectNameRequired = "page.form.projectNameRequired"

	PageFormPathRequired = "page.form.pathRequired"

	BulkRedirectNoneSelected = "page.bulk.redirectNoneSelected"
	BulkRedirectAllDeleted   = "page.bulk.redirectAllDeleted"
	BulkRedirectAllSkipped   = "page.bulk.redirectAllSkipped"
	BulkRedirectPartial      = "page.bulk.redirectPartial"
)

const MsgInternalError = response.InternalKey

const (
	PageKindHome     = "home"
	PageKindPage     = "page"
	PageKindArticle  = "article"
	PageKindTag      = "tag"
	PageKindArchive  = "archive"
	PageKindSearch   = "search"
	PageKindNotFound = "notFound"
)

func PageKinds() []string {
	return []string{
		PageKindArchive,
		PageKindArticle,
		PageKindHome,
		PageKindNotFound,
		PageKindPage,
		PageKindSearch,
		PageKindTag,
	}
}

func ValidatePageContentContract(kind, targetType string, targetID *string) bool {
	if targetID != nil && strings.TrimSpace(*targetID) == "" {
		return false
	}
	noTarget := targetType == "none" && targetID == nil
	hasTarget := func(expected string) bool {
		return targetType == expected && targetID != nil && strings.TrimSpace(*targetID) != ""
	}
	switch kind {
	case PageKindHome, PageKindArchive, PageKindSearch, PageKindNotFound:
		return noTarget
	case PageKindPage, PageKindArticle, PageKindTag:
		return hasTarget(kind)
	default:
		return false
	}
}

// StatusOf 是这些业务错误的状态码**唯一事实源**：Err(key) 在声明处就把状态包进去，
// 出口（response.ErrorStatusAuto）只读声明，不再有一层手写的 errors.Is 映射表。
// 分类按 AGENTS：400 参数错 / 404 不存在 / 409 冲突与版本 / 503 依赖不可用。
// 未登记的 key（ErrActionTargetInUse、Msg* 界面词条）返回 0，表示「不声明」，由出口兜底。
func StatusOf(key string) int {
	switch key {
	case ErrInvalidParam, ErrInvalidKind, ErrInvalidDocument, ErrInvalidPath,
		ErrProjectRequired, ErrInvalidSlot, ErrRedirectLoop, ErrRedirectTargetMiss,
		ErrBlueprintInvalid:
		return 400
	case ErrPageNotFound, ErrProjectNotFound, ErrRollbackTargetMiss,
		ErrRevisionNotFound, ErrRedirectNotFound, ErrSlotPageMiss:
		return 404
	case ErrNoStagedArtifact, ErrDraftVersionConflict, ErrPathOccupied, ErrRebuildRequired,
		ErrInputMoved, ErrPageLangExcluded, ErrCannotExcludeDefaultLang,
		ErrLangAlreadyExcluded, ErrLangNotExcluded, ErrRedirectConflict, ErrRedirectOccupied:
		return 409
	case ErrBlueprintUnavailable, ErrRebuildOverloaded, ErrRedirectUnavailable:
		return 503
	default:
		return 0
	}
}

func Err(key string) error {
	err := facing.New(key)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(err, status)
	}
	return err
}

// ActionTargetInUse 单独包 409：它的 key 是点分形式，但状态码不在 StatusOf 表里
// （那张表只覆盖裸常量名的业务错误），所以保留这条显式包装，避免改走 StatusOf 后丢状态。
func ActionTargetInUse() error { return facing.WithStatus(Err(ErrActionTargetInUse), 409) }
