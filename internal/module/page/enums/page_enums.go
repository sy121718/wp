package pageenums

import "go_wp/pkg/facing"

import "strings"

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

const MsgInternalError = "MsgInternalError"

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

var errFallbacks = map[string]string{
	ErrActionTargetInUse:        "这个目标仍被页面或区块引用，请先替换引用后再删除",
	ErrPageLangExcluded:         "该语言已从这一页的产出范围里排除，请先恢复再发布",
	ErrCannotExcludeDefaultLang: "站点默认语言不能排除：产物的原文就是它",
	ErrLangAlreadyExcluded:      "该语言已经被排除过了",
	ErrLangNotExcluded:          "该语言没有被排除，无需恢复",
	ErrInvalidParam:             "请求参数无效",
	ErrPageNotFound:             "页面不存在",
	ErrProjectNotFound:          "工程不存在",
	ErrProjectRequired:          "请先选择要操作的站点工程",
	ErrInvalidKind:              "不支持的页面类型",
	ErrInvalidDocument:          "页面草稿文档不合法",
	ErrInvalidPath:              "页面访问路径不合法",
	ErrDraftVersionConflict:     "草稿版本已更新，请刷新后重试",
	ErrPathOccupied:             "页面访问路径已被占用",
	ErrRevisionNotFound:         "修订版本不存在或已被清理",
	ErrBlueprintUnavailable:     "蓝图能力未接入，暂时无法从蓝图创建页面",
	ErrBlueprintInvalid:         "蓝图不存在或没有可用版本，请换一个蓝图",
	ErrNoStagedArtifact:         "无暂存产物，请先构建",
	ErrRollbackTargetMiss:       "回滚目标产物不存在",
	ErrRebuildRequired:          "草稿已变更，请重新构建后再发布",
	ErrInputMoved:               "编译期间内容已更新，本次发布已取消，请重新构建后再发布",
	ErrRebuildOverloaded:        "本轮需要重建的页面超过单批上限，其余已排入队列",
	ErrInvalidSlot:              "槽位键不在允许范围内，请回到列表页重新选择",
	ErrSlotPageMiss:             "要绑定的页面不存在、已被删除，或不属于当前工程",
	ErrRedirectUnavailable:      "重定向能力未接入，暂时无法管理重定向",
	ErrRedirectNotFound:         "该重定向不存在，可能已被删除",
	ErrRedirectOccupied:         "源路径已被占用，不能为它重复配置重定向",
	ErrRedirectTargetMiss:       "目标路径不存在或尚未激活，请先确认目标页面已发布",
	ErrRedirectLoop:             "该重定向会形成循环跳转，已拒绝",
	ErrRedirectConflict:         "该重定向刚被其它操作改动，请刷新后重试",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }

func ActionTargetInUse() error { return facing.WithStatus(Err(ErrActionTargetInUse), 409) }
