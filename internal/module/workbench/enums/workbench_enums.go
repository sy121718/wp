package workbenchenums

import (
	"net/http"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

const MsgDashboardTitle = "MsgDashboardTitle"

const MsgInternalError = response.InternalKey

const MsgCompileFailed = "MsgCompileFailed"

const ErrStructureTemplateFieldBinding = "workbench.err.structureTemplateFieldBinding"

const ErrPreviewFieldBindingUnsupported = "workbench.err.previewFieldBindingUnsupported"

const ErrPreviewActionTargetUnavailable = "workbench.err.previewActionTargetUnavailable"

const ErrPreviewDocumentInvalid = "workbench.err.previewDocumentInvalid"

const ErrTemplateEntityTypeMismatch = "workbench.err.templateEntityTypeMismatch"

const ErrTemplateFieldBindingInvalid = "workbench.err.templateFieldBindingInvalid"

const ErrTemplateProjectScope = "workbench.err.templateProjectScope"

const ErrInstanceNoDocument = "workbench.err.instanceNoDocument"

const ErrInstanceSaveRejected = "workbench.err.instanceSaveRejected"

const ErrMissingPageID = "workbench.err.missingPageId"

const ErrPageNotFound = "workbench.err.pageNotFound"

const ErrMissingBlockID = "workbench.err.missingBlockId"

const ErrBlockNotFound = "workbench.err.blockNotFound"

const ErrTemplateNotFound = "workbench.err.templateNotFound"

const ErrTemplateParamRequired = "workbench.err.templateParamRequired"

const ErrPreviewParamsRequired = "workbench.err.previewParamsRequired"

const ErrPreviewParamsIncomplete = "workbench.err.previewParamsIncomplete"

const ErrPreviewEntityIDRequired = "workbench.err.previewEntityIdRequired"

const ErrDraftDecodeFailed = "workbench.err.draftDecodeFailed"

const ErrDraftVersionStale = "workbench.err.draftVersionStale"

const ErrTemplateDocumentEmpty = "workbench.err.templateDocumentEmpty"

const ErrInstanceNotFound = "workbench.err.instanceNotFound"

const ErrInstanceEditNotAssembled = "workbench.err.instanceEditNotAssembled"

const ErrContentTemplateEditNotAssembled = "workbench.err.contentTemplateEditNotAssembled"

const ErrContentTemplatePreviewNotAssembled = "workbench.err.contentTemplatePreviewNotAssembled"

const ErrStructureTemplatePreviewNotAssembled = "workbench.err.structureTemplatePreviewNotAssembled"

const ErrDraftEncodeFailed = "workbench.err.draftEncodeFailed"

const ErrBlockEncodeFailed = "workbench.err.blockEncodeFailed"

const ErrTemplateEncodeFailed = "workbench.err.templateEncodeFailed"

const ErrEditorMetaEncodeFailed = "workbench.err.editorMetaEncodeFailed"

const ErrComponentSchemaBuildFailed = "workbench.err.componentSchemaBuildFailed"

const ErrComponentSchemaEncodeFailed = "workbench.err.componentSchemaEncodeFailed"

const ErrSaveParamsIncomplete = "workbench.err.saveParamsIncomplete"

const ErrDetachConfirmRequired = "workbench.err.detachConfirmRequired"

const ErrDraftDocumentEmpty = "workbench.err.draftDocumentEmpty"

const TitleEditor = "workbench.title.editor"

const TitleEditorPrefix = "workbench.title.editorPrefix"

const TitleBlockPrefix = "workbench.title.blockPrefix"

const TitleTemplatePrefix = "workbench.title.templatePrefix"

const TitleInstance = "workbench.title.instance"

const ModeFollowTemplate = "workbench.mode.followTemplate"

const ModeDocument = "workbench.mode.document"

const (
	DashboardPageKindHome     = "admin.dashboard.pageKind.home"
	DashboardPageKindPage     = "admin.dashboard.pageKind.page"
	DashboardPageKindArticle  = "admin.dashboard.pageKind.article"
	DashboardPageKindTag      = "admin.dashboard.pageKind.tag"
	DashboardPageKindArchive  = "admin.dashboard.pageKind.archive"
	DashboardPageKindSearch   = "admin.dashboard.pageKind.search"
	DashboardPageKindNotFound = "admin.dashboard.pageKind.notFound"
)

const (
	DashboardTrendGranularityPrefix = "admin.dashboard.trend.granularity."
	DashboardTrendTitlePrefix       = "admin.dashboard.trend.title."
	DashboardRangePrefix            = "admin.dashboard.range."
)

// StatusOf 声明每个业务错误 key 的真实 HTTP 状态码：出口只调
// response.ErrorStatusAuto，由声明处决定状态，handler 不再逐处硬写。
// 未登记的 key 返回 0，走出口第 ②③ 档（业务事实 400 / 其余 500）。
func StatusOf(key string) int {
	switch key {
	case ErrMissingPageID, ErrMissingBlockID, ErrTemplateParamRequired,
		ErrPreviewParamsRequired, ErrPreviewParamsIncomplete, ErrPreviewEntityIDRequired,
		ErrDraftDecodeFailed, ErrDraftDocumentEmpty, ErrTemplateDocumentEmpty,
		ErrSaveParamsIncomplete, ErrInstanceSaveRejected:
		return http.StatusBadRequest
	case ErrPageNotFound, ErrBlockNotFound, ErrTemplateNotFound, ErrInstanceNotFound:
		return http.StatusNotFound
	case ErrDetachConfirmRequired:
		return http.StatusConflict
	case ErrInstanceEditNotAssembled, ErrContentTemplateEditNotAssembled,
		ErrContentTemplatePreviewNotAssembled, ErrStructureTemplatePreviewNotAssembled:
		return http.StatusServiceUnavailable
	case ErrInstanceNoDocument:
		return http.StatusUnprocessableEntity
	case ErrDraftEncodeFailed, ErrBlockEncodeFailed, ErrTemplateEncodeFailed,
		ErrEditorMetaEncodeFailed, ErrComponentSchemaBuildFailed, ErrComponentSchemaEncodeFailed:
		return http.StatusInternalServerError
	}
	return 0
}

func Err(key string) error {
	err := facing.New(key)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(err, status)
	}
	return err
}
