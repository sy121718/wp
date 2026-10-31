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

var errFallbacks = map[string]string{
	ErrRowCountMismatch:   "提交的行数不一致，请刷新后重试",
	ErrStorageUnavailable: "译文存储不可用",
	ErrContextInvalid:     "语境非法：导航标签只接受 %s",
	ErrSourceChanged:      "菜单文字已变化，请刷新后重试",
	ErrNotTranslatable:    "%s：该菜单文字不参与翻译（纯数字或纯符号）",
	ErrSaveFailed:         "保存失败：%s",
	ErrProjectListFailed:  "读取工程列表失败，请稍后重试",
	ErrInvalidParam:       "请求参数无效",
	ErrNotFound:           "请求资源不存在",
	ErrInvalidKind:        "不支持的页面类型",
	ErrPathTaken:          "同工程同类型下已存在相同路径的导航项",
	ErrInvalidSource:      "非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体",
	ErrInvalidTarget:      "非法的打开方式，仅支持 self 或 blank",
	ErrInvalidParent:      "父级菜单不合法：父项必须存在、与本项属于同一工程与同一类型，且不能形成循环",
	ErrProjectRequired:    "请先选择要操作的站点工程",
	ErrStaleVersion:       "该菜单项已被其他窗口修改，请刷新后重试",
	ErrSortTied:           "同一级里存在重复的排序值，本次没有产生位移；请先把这一级的顺序理顺",
	ErrSourceKindFailed:   "导航菜单原文读取失败，请稍后重试",

	ErrTranslationDuplicate:        "同一菜单文字在一批提交里出现了多次，请只保留一行",
	ErrTranslationWriteFailed:      "译文保存失败，请稍后重试",
	ErrTranslationWriteUnavailable: "译文写入服务未就绪，请联系管理员",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func FallbackFor(key string) string { return errFallbacks[key] }

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
