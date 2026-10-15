package contenttemplatehttp

// content_template_err.go — 内容模板后台页的错误文案归口（第三波 CQ-009 形态 ②③ 收口）。
//
// 背景：内容模板页此前把 err.Error() 直接铺进两处 ——
//
//	形态② `c.Redirect(..., contentTemplatesListPath+"...&err="+url.QueryEscape(serr.Error()))`
//	      与 `q.Set("err", berr.Error())`：页面把 ?err= 原样渲染（admin/content_templates.html
//	      的 `{{.Err}}`），等于把数据库原文摆在运营眼前；
//	形态③ `SampleErr` 塞进模板数据：同上，且连 URL 都不用（渲染即泄漏）。
//
// 两者与 JSON body 一样**不是可信边界**：contenttemplate 契约把错误当字符串返回，
// 未命中的多半是 GORM / PG 原文（表名 content_templates、约束名、SQLSTATE）。
//
// 判据只有一条（**模块不再持有自己的白名单**）：
//
//	① 可透出性 —— adminui.PageErrorFact，判据是 pkg/response.IsBusinessError 的**形态**
//	   （`模块.类别.语义` 的 key，或 `Err*` / `Msg*` 常量名）；enums 的值就是 i18n key，
//	   所以「认得出形态 = 可以直接给用户看」，认不出则记日志 + 归口；
//	② 归口文案 —— adminui.MsgInternalError 词条（缺词条回落中文原文）；
//	③ 结构化日志 —— logger.Scene + user_id + 原始错误（原文只进日志，由 ① 那条路径记）。
//
// 原先这里挂着 contentTemplateFacingMessages 白名单 + 逐条 strings.IndexByte 比对：
// 那是**第二份真相** —— service 换了措辞、或某个 key 改由别的模块提供，白名单就静默失配，
// 而两种症状都不报错（有时把内部错误当业务文案透出、有时把业务文案吞成归口）。
//
// 传输通道：写动作的结论**不再经查询参数回带**（原先 303 + `?err=` / `?done=`），
// 改由 contentTemplateJump 渲染整页提示（shell.RenderJump，文案走响应体）。那条通道要求
// 读侧再判一次「这条提示是不是本仓给的」（contentTemplateNoticeTexts 的候选集合），
// 而查询参数不是可信边界 —— 现在读侧判定（contentTemplatePageErr / contentTemplatePageDone /
// contentTemplateNoticeTexts / contentTemplateLocalNotices）整批删除。
//
// 控制器**不取词**：提示页正文是事实（key + 中文兜底 + `%s` 参数），取词在渲染期由模板注入的
// tf 完成（见 adminui.Prepare）。语言是请求级的，Go 侧取词意味着每处都要再想一遍「这句属于
// 哪个语言」，而漏一处的症状是「中文环境全对、英文界面上整句变中文」——不报错、不记日志、测试也不红。

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/backref"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/internal/shell"
)

// contentTemplateErrScene 日志场景名（与 content_template_handle.go 同源）。
const contentTemplateErrScene = "content_template"

// contentTemplateErrInternalFallback 归口文案的中文兜底。
//
// 取值直接**引用共享出口**的那一份（adminui.InternalMsg），不在这里再写一遍字面量 ——
// 中文兜底一旦两处各存一份，改一处就会出现「页面上的归口提示与 JSON 出口不是同一句」，
// 而这种分叉不报错、不记日志，测试也不会红（两处各自看都对）。
var contentTemplateErrInternalFallback = adminui.InternalMsg().Fallback

// contentTemplateMsg 页面出口的一句文案（成功回执 / 失败原因 / 链接文字）。
//
// 就是共享形状 adminui.Msg（key + 中文兜底 + `%s` 位置参数）——用别名而不是自造 struct：
// 模块各造一份同形类型就是多份契约，字段名与参数编码迟早分叉，而症状是
// 「某个页面的提示偶尔少一个数字」，既不报错也不记日志（同 page 模块的 pageMsg）。
type contentTemplateMsg = adminui.Msg

// contentTemplateFacingFallbacks 本模块认识的 key → 中文兜底。
//
// **只是缺词条时的展示原文，不是可透出性判据**（判据只有一条，在 adminui.PageErrorFact）。
// 漏登记一条的后果是一句英文常量名（可见、可查）；反过来拿它当白名单用就退回了「第二份真相」。
//
// 这些 key 都是**运营能自己处理**的：模板不存在 / 被实例引用 / 文档格式非法 / 工程选错。
var contentTemplateFacingFallbacks = map[string]string{
	contenttemplateenums.ErrInvalidParam: "参数不完整，请检查工程、模板名与实体类型。",
	contenttemplateenums.ErrNotFound:     "这套模板不存在，可能已被删除。",
	// 页面文档里的 settings.structure 绑定与实例的 template_id 共用这条 key：
	// 两者的处置都是「先去那个引用方解绑」，而明细（页面名 / 实例实体）在服务日志里。
	contenttemplateenums.ErrTemplateInUse:          "这套模板仍被页面或自动发布实例引用，不能删除（引用方见列表的「引用」列）。",
	contenttemplateenums.ErrStructureTemplateInUse: "这套结构模板仍被其它模板绑定为页眉 / 页脚，不能删除（受影响的模板已记入服务日志）。",
	contenttemplateenums.ErrInvalidType:            "不支持的内容类型。",
	contenttemplateenums.ErrDataInvalid:            "模板文档格式非法：请回到工作台重新保存后再试。",
	contenttemplateenums.ErrFieldBindingInvalid:    "模板里的字段绑定越界：请回到工作台改用本模板数据源内的字段。",
	contenttemplateenums.ErrProjectRequired:        "站点里有多个工程，请显式选择这套模板所属的工程。",
	contenttemplateenums.ErrProjectNotFound:        "选择的站点工程不存在，请刷新后重试。",
}

// contentTemplateFacingFallback 已知 key 的中文兜底；不认识时返回空串（由调用方回落事实自带的兜底）。
func contentTemplateFacingFallback(key string) string {
	return contentTemplateFacingFallbacks[strings.TrimSpace(key)]
}

// contentTemplateErrMsg service 错误 → 一句结论。
//
// 判定与日志都归 adminui.PageErrorFact：命中业务错误形态 → 该 key 的译文（缺词条回落本文件的
// 中文兜底）；其余 → 记结构化日志（带 user_id / 路径 / 原文）+ 归口文案。
//
// service 会用 `fmt.Errorf("%s: %w", enumsKey, err)` 把「枚举 + 明细」打成一条错误，
// 整体过不了形态判据（pkg/response 的正则是**锚定的**）—— PageErrorFact 已经按冒号切一刀、
// 拿前段判定，明细段（fact.Tail）由本模块自己决定要不要透出：这里**一律不透出**
// （明细可能是 GORM / PG 原文，只进日志），与改造前逐字一致。
func contentTemplateErrMsg(c *gin.Context, err error) contentTemplateMsg {
	if err == nil {
		return contentTemplateMsg{}
	}
	fact := adminui.PageErrorFact(c, contentTemplateErrScene, err)
	if !fact.Business {
		// 归口事实（key = MsgInternalError + 中文兜底），不是空串 —— 提示页不能显示空白。
		return contentTemplateMsg{Key: fact.Key, Fallback: fact.Fallback}
	}
	fallback := contentTemplateFacingFallback(fact.Key)
	if fallback == "" {
		fallback = fact.Fallback
	}
	return contentTemplateMsg{Key: fact.Key, Fallback: fallback}
}

// contentTemplateErrText 一句结论的文本形态（JSON 批量结果 / 片段槽位用：那里没有模板取词）。
func contentTemplateErrText(c *gin.Context, err error) string {
	return adminui.MsgText(c, contentTemplateErrMsg(c, err))
}

// contentTemplateInternalText 归口文案（拿不到可展示结论的失败分支用）+ 原文只进日志。
//
// err 为 nil 时也给归口文案：页面不能显示空串（用户会以为操作成功了）。
func contentTemplateInternalText(c *gin.Context, err error) string {
	if err == nil {
		return adminui.MsgText(c, adminui.InternalMsg())
	}
	return adminui.ErrorFactText(c, adminui.PageErrorFact(c, contentTemplateErrScene, err))
}

// sampleErrText 样例实体选取失败时的展示文案。
//
// hint 是**可行动提示**（依赖没装配 / 工程内还没有可预览的实体 / 不支持该实体类型），
// 原样可见；err 是依赖错误（products.List / contents.List 上抛，可能是 PG 原文）→
// 记日志 + 归口文案。两者分开是这一页的关键：样例实体要读两个别的模块的表，
// 那些错误里带着表名与 SQLSTATE，而这一页的提示位（SampleErr / 编辑入口提示页）都会被原样渲染。
func sampleErrText(c *gin.Context, hint string, err error) string {
	if h := strings.TrimSpace(hint); h != "" {
		return h
	}
	if err == nil {
		return ""
	}
	return contentTemplateInternalText(c, err)
}

// —— 页面自造回执与写动作出口 ——
//
// 本页自造的回执文案（不是 enums 白名单，也不来自 shell）：作为提示页正文的事实，
// key 为空 + 中文兜底 = 成品串 —— 这几条目前没有 seed 词条（中文当 key 必然查不到，
// 库内没有中文 item_key），所以**不给 Key 而是给成品兜底**：页面上显示的仍是原句中文，
// 将来补了词条只需把 Key 填上，调用点不动（同 page 的成品串档，见 adminui/msg.go 的三段判据）。
const (
	contentTemplateMissingIDText      = "缺少模板 id"
	contentTemplateNotFoundText       = "模板不存在"
	contentTemplateSampleMissingText  = "缺少预览样例实体"
	contentTemplateHintProductNoMod   = "商品模块未装配，无法自动选取预览样例"
	contentTemplateHintContentNoMod   = "内容模块未装配，无法自动选取预览样例"
	contentTemplateHintNoProduct      = "工程内还没有商品，无法预览商品详情模板"
	contentTemplateHintNoArticle      = "工程内还没有文章，无法预览文章详情模板"
	contentTemplateHintEntityTypeMiss = "暂不支持该实体类型的预览样例自动选取（本页只支持商品与文章）"
	// 切换生效的成功回执。生效的那套换了意味着引用它的产物过期，
	// 回执把这件事说出来，免得有人以为「只是改了个标记」。
	contentTemplateActivateDoneText = "已切换生效模板，引用它的页面与实例会重新构建。"
	// 引用反查未装配时的提示。刻意**不写成「系统内部错误」**：正确读法是「查不出来」，
	// 而不是一次失败；而且它与「没有引用」必须长得不同 —— 后者会让人以为可以放心删。
	contentTemplateImpactUnavailableText = "引用反查能力未装配：本页无法列出引用这套模板的页面与实例，删除前请人工确认。"
)

// 页面自造回执的一句话（正文即兜底，Key 为空）。
var (
	contentTemplateMissingIDMsg     = contentTemplateMsg{Fallback: contentTemplateMissingIDText}
	contentTemplateNotFoundMsg      = contentTemplateMsg{Fallback: contentTemplateNotFoundText}
	contentTemplateSampleMissingMsg = contentTemplateMsg{Fallback: contentTemplateSampleMissingText}
)

// contentTemplateImpactLoadFailedKey / Fallback 工程列表装载失败时的引用面提示（i18n key + 中文兜底）。
//
// 与上面那条**分开**，因为归因不同、处置也不同：那条是「本页永远查不了（能力没装）」，
// 这条是「这一次没读出来（工程列表没读到），刷新可能就好了」。把后者说成「能力未装配」
// 是本类缺陷的同一个形状 —— 用一句不准确的话把人支向错的方向（去查部署，而实际只需刷新）。
const (
	contentTemplateImpactLoadFailedKey      = contenttemplateenums.ImpactLoadFailed
	contentTemplateImpactLoadFailedFallback = "本次没取到引用面（工程列表未读到），删除前请人工确认。"
)

// contentTemplateImpactLoadFailedText 装载失败时的引用面提示（当前语言文本）。
//
// 片段槽位：这个键在模板里以文本形态渲染（data["ImpactNote"]），因此在这里取成文本；
// 取词走 adminui.MsgText（不再经 shell）。
func contentTemplateImpactLoadFailedText(c *gin.Context) string {
	return adminui.MsgText(c, contentTemplateImpactLoadFailedMsg)
}

var contentTemplateImpactLoadFailedMsg = contentTemplateMsg{
	Key: contentTemplateImpactLoadFailedKey, Fallback: contentTemplateImpactLoadFailedFallback,
}

// contentTemplateImpactUnparsableTemplate 影响面可能不完整的提示（%d = 无法解析的文档数）。
//
// 带占位符的受控文案（与批量结论同一形态），只在页面数据（ImpactNote）里渲染。
const contentTemplateImpactUnparsableTemplate = "有 %d 份文档无法解析，影响面可能不完整（这些文档仍可能引用本模板）。"

// contentTemplateBackMsg 提示页「立即前往」链接的文字（复用列表页标题词条，不新增全站词条）。
//
// 包级变量并**不带 gin.Context**：这段文案属于哪个语言，由取词发生的时刻决定，
// 与控制器无关 —— 这正是本批的形状要求（同 page 的 pageListBackMsg）。
var contentTemplateBackMsg = contentTemplateMsg{
	Key: contentTemplateBackTextKey, Fallback: contentTemplateBackTextFallback,
}

// contentTemplateBackTextKey / Fallback 列表页标题词条（链接文字复用，不新增全站词条）。
const (
	contentTemplateBackTextKey      = contenttemplateenums.Heading
	contentTemplateBackTextFallback = "内容模板"
)

// contentTemplateJumpKeys 写动作回跳要透传的筛选键（与表单 action / 编辑链接上的键一致）。
var contentTemplateJumpKeys = []string{"project", "entityType"}

// contentTemplateJump 页面写动作 / 编辑入口的统一出口：整页提示（对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 303 + `?err=` / `?done=`：那条通道要求读侧再判一次「这条提示是不是本仓给的」
// （contentTemplateNoticeTexts 的候选集合），而查询参数不是可信边界。现在文案走响应体，
// 读侧判定（contentTemplatePageErr / contentTemplatePageDone / contentTemplateNoticeTexts）随之整批删除。
//
// msg 是**事实**（key + 中文兜底 + `%s` 参数），不是拼好的句子 —— 取词由模板注入的 tf 做，
// 判定与日志归 adminui.PageErrorFact（见文件头）。原文只进日志：换个页面呈现不等于可以把
// err.Error() 铺在页面上。
// 回跳地址由 backref.BackPath 从表单 action 的 query 按白名单读回（不再用隐藏域塞整串 URL）。
// 失败不自动跳转（Seconds=0）：运营要看清楚原因；成功 1 秒后回列表页。
func contentTemplateJump(c *gin.Context, ok bool, msg contentTemplateMsg) {
	back := backref.BackPath(c, contentTemplatesListPath, contentTemplateJumpKeys...)
	backText := contentTemplateBackMsg
	if ok {
		shell.RenderJump(c, shell.Jump{
			OK: true, MsgKey: msg.Key, Msg: msg.Fallback, MsgArgs: msg.Args,
			Back: back, BackTextKey: backText.Key, BackText: backText.Fallback, Seconds: 1,
		})
		return
	}
	shell.RenderJump(c, shell.Jump{
		MsgKey: msg.Key, Msg: msg.Fallback, MsgArgs: msg.Args,
		Back: back, BackTextKey: backText.Key, BackText: backText.Fallback,
	})
}
