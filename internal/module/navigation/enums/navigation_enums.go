package navigationenums

import (
	"fmt"
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
)

func Err(key string) error { return facing.New(key) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

const (
	TranslationHeading = "admin.navigation_translations.heading"

	Saved     = "admin.navigation_translations.saved"
	SavedNone = "admin.navigation_translations.savedNone"

	ErrRowCountMismatch   = "admin.navigation_translations.err.rowCountMismatch"
	ErrStorageUnavailable = "admin.navigation_translations.err.storageUnavailable"
	ErrContextInvalid     = "admin.navigation_translations.err.contextInvalid"
	ErrSourceChanged      = "admin.navigation_translations.err.sourceChanged"
	ErrNotTranslatable    = "admin.navigation_translations.err.notTranslatable"
	ErrSaveFailed         = "admin.navigation_translations.err.saveFailed"
	ErrProjectListFailed  = "admin.navigation_translations.err.projectListFailed"

	SavedFallback = "已保存 %s 条译文；已标记受影响页面待重建（下次构建生效）。"

	Heading              = "admin.navigations.heading"
	ActionDone           = "admin.navigations.actionDone"
	NoPublicPath         = "admin.navigations.no_public_path"
	NoticeNoSourcePicked = "admin.navigations.notice.noSourcePicked"

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

func SavedMsg(written string) facing.Msg {
	return facing.Msg{Key: Saved, Fallback: SavedFallback}.WithArgs(written)
}
