package workbenchhttp

// workbench_err.go — 工作台「预览 / 编译 422」的文案归口（迁移 292）。
//
// 背景：结构模板（页眉 / 页脚）若文档里带了字段绑定（旧脏数据，或画布上直接拖入带绑定的组件后
// 走**草稿预览** —— 保存期有校验、草稿预览没有），预览与编译入口会以 HTTP 422 + 一句泛化的
// 「预览编译失败」暴露。状态码刻意保留 422（这是作者能自己处理的配置问题，不是服务端故障），
// 缺的是**可归因**：作者拿到那句话既不知道错在哪，也不知道下一步做什么。
//
// 三件套（与 admin 的 admin_err.go 同形；page 那份已收敛到 internal/adminui.PageErrorFact）：
//
//	① 声明处兜底 —— workbenchenums.FallbackFor：只取已声明错误的中文兜底。
//	              没登记 = 不给用户看（走归口文案），漏登记只会少一句提示（一眼可见），
//	              而漏判的方向恰好相反 —— 内部字符串只要长得像 key 就会被透出。
//	② 归口文案 —— workbenchenums.MsgCompileFailed（缺词条回落中文原文），
//	              未命中白名单时**不**拼接任何错误原文。
//	③ 结构化日志 —— logger.Scene("workbench") + path + doc_kind + 原始错误。
//	              原文只进日志：编译错误里带节点路径与模板片段，铺在画布上等于公开内部结构。
//
// 分类判据分三种（都不是「嗅探错误散文」）：
//   - **类型标记**：page/service 对「作者可操作的组件校验问题」打了
//     pagecontract.PreviewProblem（见 writePreviewCompileRejected 的 ①）—— 这是主路径，
//     结构化、可 errors.As；
//   - 预览编译（renderPreview）的其余失败按**错误链事实与文档校验**判：组件是否真的缺少内容解析器、文档有没有过校验；
//   - 模板预览（renderTemplatePreview）按**模块哨兵 key** 判：presentation / contenttemplate
//     的哨兵是字符串常量（errors.New(<常量>) 在各调用点新建），errors.Is 拿不到同一个值 ——
//     与 workbench_page.go 判 ErrDetachConfirmRequired 是同一口径，匹配的是代码里定义的
//     key，不是人写的中文句子。

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// workbenchErrScene 结构化日志场景名（与工作台其它 logger.Scene 一致）。
const workbenchErrScene = "workbench"

// workbenchCompileFallback 归口文案的中文兜底（词条 MsgCompileFailed，值与它逐字一致）。
//
// 它是**归口**：只兜「归不了因」的失败 —— 装配缺失、组件模板加载失败、导航解析器未注入
// 这类内部问题（原文只进结构化日志）。能归因的两类各有出口：作者可操作的组件校验提示走
// PreviewProblem 分支（带原文），错误链事实或文档校验能说清的走 workbench.err.*。
//
// 兜底值与 058 已 seed 的词条值相同，是为了让「i18n 已初始化 / 未初始化」两种环境下
// 响应体一致 —— 否则测试里看到的是兜底长句、生产里看到的是词条短句。
const workbenchCompileFallback = "预览编译失败"

// workbenchFacingText 按已分类的错误key取声明处兜底与当前语言文本；未声明则归口。
//
// 取词直接经 i18n.TranslateFunc，语言取自请求；这些成品文本会进
// 预览 iframe 的 body 与 JSON message，不经过 pkg/response 的翻译层。
func workbenchFacingText(c *gin.Context, key string) string {
	fallback := workbenchenums.FallbackFor(key)
	if fallback == "" {
		return ""
	}
	return i18n.TranslateFunc(response.RequestLanguage(c))(key, fallback)
}

// workbenchCompileFallbackText 编译失败的归口文案（词条 MsgCompileFailed，缺词条回落中文）。
func workbenchCompileFallbackText(c *gin.Context) string {
	return i18n.TranslateFunc(response.RequestLanguage(c))(workbenchenums.MsgCompileFailed, workbenchCompileFallback)
}

// writePreviewCompileRejected 预览编译失败的 422 出口（画布 iframe / 新标签预览共用）。
//
// 状态码不变（422 对作者是业务信息）；变的是**判据**：按错误链上的类型标记分级，
// 而不是把 err.Error() 拼进响应。
//
//	① errors.As 命中 *pagecontract.PreviewProblem —— 组件校验问题（「手风琴至少需要一个
//	   折叠项」这类），带原文透出：这是工作台画布的核心价值，作者据此直接在画布上修；
//	② 真实编译错误链或文档校验能说清的 —— 走 workbench.err.* 可行动文案；
//	③ 其余（装配缺失、组件模板加载失败）—— 归口文案，**原文只进结构化日志**。
//
// 先判 ① 再判 ②：带标记的问题优先（校验在编译里最先发生，作者先修它再看别的）。
func writePreviewCompileRejected(c *gin.Context, document json.RawMessage, kind workbenchservice.PreviewDocKind, err error) {
	// 原文一律进日志：① 的文本虽然也给作者看，但日志里保留完整包装链（含前缀与节点路径）
	// 才能还原「哪一次编译、哪种文档」，与响应体的取舍无关。
	logger.Scene(workbenchErrScene).
		With("path", c.Request.URL.Path).
		With("doc_kind", workbenchservice.PreviewDocKindName(kind)).
		Error(err, "预览编译失败")

	// 判据（类型标记 ① → 错误链事实与校验 ② → 归不了因 ③）全在 service.ClassifyPreviewRejection：
	// 它按错误链与文档事实给回「可直接透出的原文」或「可行动文案的 key」，
	// 本函数只负责把它翻成状态码与响应体（与画布其它出口同一形态）。
	problem, key := workbenchservice.ClassifyPreviewRejection(document, kind, err)
	if problem != "" {
		c.String(http.StatusUnprocessableEntity, workbenchCompileFallbackText(c)+"："+problem)
		return
	}
	if key != "" {
		if text := workbenchFacingText(c, key); text != "" {
			c.String(http.StatusUnprocessableEntity, text)
			return
		}
	}
	// ③ 归口。
	c.String(http.StatusUnprocessableEntity, workbenchCompileFallbackText(c))
}

// —— 画布出口的受控短句（迁移 452）——

// workbenchShortFallbacks 受控短句的中文兜底（键 = workbenchenums 常量，值 = 包内中文原文）。
//
// 与枚举声明处的分工：可归因长文案由声明处给（错在哪 + 怎么办），
// 这张表是画布出口的**固定短句** —— 少一个参数、记录不存在、能力未装配、序列化失败。
//
// 为什么单独一张表而不是塞进调用点：调用点有四十余处，`c.String(code, key, "中文")`
// 这种两参写法一多，改文案时必然出现「同一个 key 在几处配了不同兜底」—— 而兜底与词条
// 不一致的后果是「未跑迁移的环境」与「跑过的环境」显示不同句子（058 那条注释同理）。
var workbenchShortFallbacks = map[string]string{
	// 参数与记录缺失（4xx：请求方给的目标不完整或不存在）。
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
	// 能力未装配（503：装配缺陷，用户无能为力，但句子要说明这一点）。
	workbenchenums.ErrInstanceEditNotAssembled:             "实例编辑能力未装配",
	workbenchenums.ErrContentTemplateEditNotAssembled:      "内容模板编辑能力未装配",
	workbenchenums.ErrContentTemplatePreviewNotAssembled:   "内容模板预览能力未装配",
	workbenchenums.ErrStructureTemplatePreviewNotAssembled: "无实体模板预览能力未装配",
	// 内部失败（5xx：原文只进日志，这里给受控短句）。
	workbenchenums.ErrDraftEncodeFailed:           "草稿文档序列化失败",
	workbenchenums.ErrBlockEncodeFailed:           "块文档序列化失败",
	workbenchenums.ErrTemplateEncodeFailed:        "模板文档序列化失败",
	workbenchenums.ErrEditorMetaEncodeFailed:      "编辑器元数据序列化失败",
	workbenchenums.ErrComponentSchemaBuildFailed:  "组件 schema 生成失败",
	workbenchenums.ErrComponentSchemaEncodeFailed: "组件 schema 序列化失败",
	// 画布标题与双轨状态条（模板侧不进 t 的键：这些由 Go 生成后作为 data 传给模板）。
	workbenchenums.TitleEditor:         "可视化编辑器",
	workbenchenums.TitleEditorPrefix:   "编辑器 · ",
	workbenchenums.TitleBlockPrefix:    "编辑块：",
	workbenchenums.TitleTemplatePrefix: "编辑模板：",
	workbenchenums.TitleInstance:       "自定义商品页",
	workbenchenums.ModeFollowTemplate:  "跟随模板中（模板更新会同步到这里）",
	workbenchenums.ModeDocument:        "独立文档（只影响这个商品）",
	// 结构树（outline_handle.go 拼 HTML）。
	workbenchenums.OutlineToggle:      "展开/收起",
	workbenchenums.OutlineHiddenHint:  "编辑期隐藏",
	workbenchenums.OutlineHiddenBadge: "隐",
	workbenchenums.OutlineLockedHint:  "已锁定",
	workbenchenums.OutlineLockedBadge: "锁",
	workbenchenums.OutlineOpUp:        "上移",
	workbenchenums.OutlineOpDown:      "下移",
	workbenchenums.OutlineOpDup:       "复制",
	workbenchenums.OutlineOpDel:       "删除",
	// 检查器分组标题。
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
	// 检查器字段标签与占位符。
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
	// 导航选择器。
	workbenchenums.InspectorNavAny:              "（不限）",
	workbenchenums.InspectorNavKindHeader:       "页眉",
	workbenchenums.InspectorNavKindHeaderMobile: "页眉（移动端）",
	workbenchenums.InspectorNavKindFooter:       "页脚",
	workbenchenums.InspectorNavKindFooterMobile: "页脚（移动端）",
	// 重复项面板。
	workbenchenums.InspectorRepeaterMoveUp:   "上移（面板一起移动）",
	workbenchenums.InspectorRepeaterMoveDown: "下移（面板一起移动）",
	workbenchenums.InspectorRepeaterRemove:   "删除该{noun}（同时删除对应面板）",
	workbenchenums.InspectorRepeaterMatched:  "{noun}与面板数量一致（{count}）：↑ ↓ 可整体调序，面板内容在画布中编辑。",
	workbenchenums.InspectorRepeaterMismatch: "数量不一致（{noun} {rows} 个 / 面板 {panels} 个），保存会被校验拦下：点「+ 添加」补齐，或删除多余标签。",
}

// workbenchShortText 取一条受控短句（key 来自 workbenchenums，兜底来自 workbenchShortFallbacks）。
//
// 未登记进表的 key 是**编码错误**：此时不能把裸 key 写进响应（英文站点上就是那串
// `workbench.err.xxx`），所以回落统一内部错误文案并记一条日志 —— 静默返回空串会让
// 画布上出现一片空白，比报错更难查。
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

// workbenchTrFunc 按 key 取词的函数（兜底来自 workbenchShortFallbacks）。
//
// 给「Go 侧拼 HTML / 结构化面板」那条链路用：检查器的分组标题、字段 Label / Placeholder
// 与结构树的按钮提示都由纯函数产出，它们不该知道 gin 上下文，所以只收一个
// 「key → 当前语言文案」的函数（fallback 固定在表里，调用点不必重复写中文）。
func workbenchTrFunc(c *gin.Context) func(key string) string {
	return func(key string) string { return workbenchShortText(c, key) }
}
