package navigationenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
)

const (
	ErrInvalidParam = "ErrInvalidParam"
	ErrNotFound     = "ErrNotFound"
	ErrInvalidKind  = "ErrInvalidKind"
	ErrPathTaken    = "ErrPathTaken"

	ErrInvalidSource = "ErrInvalidSource"

	ErrInvalidTarget = "ErrInvalidTarget"

	ErrInvalidParent = "ErrInvalidParent"

	ErrProjectRequired = "ErrProjectRequired"

	ErrStaleVersion = "ErrStaleVersion"

	ErrSortTied = "ErrSortTied"

	// ErrBatchReject 是多项批量写被整体拒绝时的公共主消息；单条请求仍给该条的具体原因。
	ErrBatchReject = "navBatchReject"
)

// StatusFor 只声明拒绝原因对应的 HTTP 状态；文案由词库统一取词。
// 未登记的 key 按内部错误处理，不把未知业务形状静默降成 400。
func StatusFor(key string) int {
	// 带参消息事实的 key 形如 `ErrX|参数`：状态只看 key 本体。
	if base, _, found := strings.Cut(key, "|"); found {
		key = base
	}
	switch key {
	case ErrNotFound:
		return http.StatusNotFound
	case ErrPathTaken, ErrStaleVersion:
		return http.StatusConflict
	case ErrInvalidParam, ErrInvalidKind, ErrInvalidSource, ErrInvalidTarget,
		ErrInvalidParent, ErrProjectRequired, ErrSortTied, ErrBatchReject, NoPublicPath:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// Err 在声明处同时给出业务状态：调用方不再各写一份 key→状态 switch。
func Err(key string) error { return facing.WithStatus(facing.New(key), StatusFor(key)) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

const (
	TranslationHeading = "admin.navigation_translations.heading"

	ErrRowCountMismatch   = "admin.navigation_translations.err.rowCountMismatch"
	ErrStorageUnavailable = "admin.navigation_translations.err.storageUnavailable"
	ErrContextInvalid     = "admin.navigation_translations.err.contextInvalid"
	ErrSourceChanged      = "admin.navigation_translations.err.sourceChanged"
	ErrNotTranslatable    = "admin.navigation_translations.err.notTranslatable"
	ErrSaveFailed         = "admin.navigation_translations.err.saveFailed"
	ErrProjectListFailed  = "admin.navigation_translations.err.projectListFailed"

	Heading      = "admin.navigations.heading"
	ActionDone   = "admin.navigations.actionDone"
	NoPublicPath = "admin.navigations.no_public_path"

	InternalKey = "navigation.err.internal"

	KindFooter       = "admin.navigations.kind.footer"
	KindFooterMobile = "admin.navigations.kind.footerMobile"
	KindHeader       = "admin.navigations.kind.header"
	KindHeaderMobile = "admin.navigations.kind.headerMobile"

	PanelBlockNameMenu         = "admin.navigations.panel.blockName.menu"
	PanelBlockNameHeader       = "admin.navigations.panel.blockName.header"
	PanelBlockNameHeaderMobile = "admin.navigations.panel.blockName.headerMobile"
	PanelBlockNameFooter       = "admin.navigations.panel.blockName.footer"
	PanelBlockNameFooterMobile = "admin.navigations.panel.blockName.footerMobile"
	PanelBlockNamePanel        = "admin.navigations.panel.blockName.panel"

	SourcePage     = "admin.navigations.source.page"
	SourceArticle  = "admin.navigations.source.article"
	SourceProduct  = "admin.navigations.source.product"
	SourceCategory = "admin.navigations.source.category"
)
