package workbenchhttp

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const workbenchErrScene = "workbench"

const workbenchCompileFallback = "预览编译失败"

func workbenchFacingText(c *gin.Context, key string) string {
	// 走统一消息出口：词库命中就用译文；缺词时 response 会记一条可定位的
	// logMissingMessage（key / lang / 调用处 / 路径）并交回归口文案。
	// 这里不能自己调 TranslateFunc 把兜底传空串 —— 那会把「缺词」变成页面上
	// 的空消息槽，既不可读也没有任何线索能查是哪条词缺了。
	return response.MsgText(c, facing.Msg{Key: key})
}

func workbenchCompileFallbackText(c *gin.Context) string {
	return i18n.TranslateFunc(response.RequestLanguage(c))(workbenchenums.MsgCompileFailed, workbenchCompileFallback)
}

func writePreviewCompileRejected(c *gin.Context, document json.RawMessage, kind workbenchservice.PreviewDocKind, err error) {

	logger.Scene(workbenchErrScene).
		With("path", c.Request.URL.Path).
		With("doc_kind", workbenchservice.PreviewDocKindName(kind)).
		Error(err, "预览编译失败")

	problem, key := workbenchservice.ClassifyPreviewRejection(document, kind, err)
	if problem != "" {
		// TODO(编码明细出口)：这里把「前缀 + problem」拼成一句，违反「编码明细只在出口
		// 逐段取词」。等 MsgCompileFailed 词条改成带 %s 的形态后，改成 key+"|"+problem，
		// 由 pkg/response 的 translate 逐段取词。
		response.ErrorWithMessage(c, http.StatusUnprocessableEntity, workbenchCompileFallbackText(c)+"："+problem)
		return
	}
	if key != "" {
		if text := workbenchFacingText(c, key); text != "" {
			response.ErrorWithMessage(c, http.StatusUnprocessableEntity, text)
			return
		}
	}

	response.ErrorWithMessage(c, http.StatusUnprocessableEntity, workbenchCompileFallbackText(c))
}

var workbenchShortFallbacks = map[string]string{

	workbenchenums.ErrMissingPageID:           "缺少页面 id",
	workbenchenums.ErrPageNotFound:            "页面不存在",
	workbenchenums.ErrMissingBlockID:          "缺少块 id",
	workbenchenums.ErrBlockNotFound:           "全局块不存在",
	workbenchenums.ErrTemplateNotFound:        "模板不存在",
	workbenchenums.ErrTemplateParamRequired:   "缺少 template",
	workbenchenums.ErrPreviewParamsRequired:   "缺少 template / entityType / entityId",
	workbenchenums.ErrPreviewParamsIncomplete: "预览参数不完整",
	workbenchenums.ErrPreviewEntityIDRequired: "缺少预览样例实体 entityId（字段绑定预览需要一条真实 " +
		"{type} 记录）",
	workbenchenums.ErrDraftDecodeFailed:     "草稿文档解析失败",
	workbenchenums.ErrDraftVersionStale:     "草稿版本已更新，请刷新后重试",
	workbenchenums.ErrTemplateDocumentEmpty: "模板文档为空",
	workbenchenums.ErrInstanceNotFound:      "实例不存在",
	workbenchenums.ErrDraftDocumentEmpty:    "草稿文档为空",
	workbenchenums.ErrSaveParamsIncomplete:  "保存参数不完整",
	workbenchenums.ErrDetachConfirmRequired: "这次改动会让本商品转为独立文档：之后模板更新不再同步到这里；" +
		"想回到跟随时，在商品详情页点「重新套用预设」即可。继续保存？",

	workbenchenums.ErrInstanceEditNotAssembled:             "实例编辑能力未装配",
	workbenchenums.ErrContentTemplateEditNotAssembled:      "内容模板编辑能力未装配",
	workbenchenums.ErrContentTemplatePreviewNotAssembled:   "内容模板预览能力未装配",
	workbenchenums.ErrStructureTemplatePreviewNotAssembled: "无实体模板预览能力未装配",

	workbenchenums.ErrDraftEncodeFailed:           "草稿文档序列化失败",
	workbenchenums.ErrBlockEncodeFailed:           "块文档序列化失败",
	workbenchenums.ErrTemplateEncodeFailed:        "模板文档序列化失败",
	workbenchenums.ErrEditorMetaEncodeFailed:      "编辑器元数据序列化失败",
	workbenchenums.ErrComponentSchemaBuildFailed:  "组件 schema 生成失败",
	workbenchenums.ErrComponentSchemaEncodeFailed: "组件 schema 序列化失败",

	workbenchenums.TitleEditor:         "可视化编辑器",
	workbenchenums.TitleEditorPrefix:   "编辑器 · ",
	workbenchenums.TitleBlockPrefix:    "编辑块：",
	workbenchenums.TitleTemplatePrefix: "编辑模板：",
	workbenchenums.TitleInstance:       "自定义商品页",
	workbenchenums.ModeFollowTemplate:  "跟随模板中（模板更新会同步到这里）",
	workbenchenums.ModeDocument:        "独立文档（只影响这个商品）",

	workbenchenums.OutlineToggle:      "展开/收起",
	workbenchenums.OutlineHiddenHint:  "编辑期隐藏",
	workbenchenums.OutlineHiddenBadge: "隐",
	workbenchenums.OutlineLockedHint:  "已锁定",
	workbenchenums.OutlineLockedBadge: "锁",
	workbenchenums.OutlineOpUp:        "上移",
	workbenchenums.OutlineOpDown:      "下移",
	workbenchenums.OutlineOpDup:       "复制",
	workbenchenums.OutlineOpDel:       "删除",

	workbenchenums.InspectorSectionContent:    "内容",
	workbenchenums.InspectorSectionStyle:      "基础",
	workbenchenums.InspectorSectionLayout:     "布局",
	workbenchenums.InspectorSectionBackground: "背景",
	workbenchenums.InspectorSectionBorder:     "边框",
	workbenchenums.InspectorSectionTransform:  "变换",
	workbenchenums.InspectorSectionMotion:     "动效",
	workbenchenums.InspectorSectionHover:      "悬停",
	workbenchenums.InspectorSectionResponsive: "响应式",
	workbenchenums.InspectorSectionAdvanced:   "高级",

	workbenchenums.InspectorCorners:           "圆角",
	workbenchenums.InspectorCornerTopLeft:     "左上",
	workbenchenums.InspectorCornerTopRight:    "右上",
	workbenchenums.InspectorCornerBottomRight: "右下",
	workbenchenums.InspectorCornerBottomLeft:  "左下",
	workbenchenums.InspectorBpDesktop:         "桌面",
	workbenchenums.InspectorBpTablet:          "平板",
	workbenchenums.InspectorBpMobile:          "手机",
	workbenchenums.InspectorDirTop:            "上",
	workbenchenums.InspectorDirRight:          "右",
	workbenchenums.InspectorDirBottom:         "下",
	workbenchenums.InspectorDirLeft:           "左",
	workbenchenums.InspectorPhClasses:         "逗号或空格分隔，禁 sky- 前缀",
	workbenchenums.InspectorPhCSSDecls:        "只写样式/布局/动画属性，分号分隔，如 font-size:16px; padding:12px",
	workbenchenums.InspectorPhDimension:       "如 16px / 1.5rem",

	workbenchenums.InspectorNavAny:              "（不限）",
	workbenchenums.InspectorNavKindHeader:       "页眉",
	workbenchenums.InspectorNavKindHeaderMobile: "页眉（移动端）",
	workbenchenums.InspectorNavKindFooter:       "页脚",
	workbenchenums.InspectorNavKindFooterMobile: "页脚（移动端）",

	workbenchenums.InspectorRepeaterMoveUp:   "上移（面板一起移动）",
	workbenchenums.InspectorRepeaterMoveDown: "下移（面板一起移动）",
	workbenchenums.InspectorRepeaterRemove:   "删除该{noun}（同时删除对应面板）",
	workbenchenums.InspectorRepeaterMatched:  "{noun}与面板数量一致（{count}）：↑ ↓ 可整体调序，面板内容在画布中编辑。",
	workbenchenums.InspectorRepeaterMismatch: "数量不一致（{noun} {rows} 个 / 面板 {panels} 个），保存会被校验拦下：点「+ 添加」补齐，或删除多余标签。",
}

func workbenchShortText(c *gin.Context, key string) string {
	fallback, ok := workbenchShortFallbacks[key]
	if !ok {
		logger.Scene(workbenchErrScene).
			With("path", c.Request.URL.Path).
			Error(errors.New("workbench 短句未登记中文兜底"), "受控短句表缺条目")
		return adminui.PageInternalText(c)
	}
	return i18n.TranslateFunc(response.RequestLanguage(c))(key, fallback)
}

func workbenchTrFunc(c *gin.Context) func(key string) string {
	return func(key string) string { return workbenchShortText(c, key) }
}
