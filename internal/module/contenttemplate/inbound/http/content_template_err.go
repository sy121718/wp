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
// 三件套（与 admin 的 admin_err.go / navigation 的 navigation_err.go 同形）：
//
//	① 白名单   —— contenttemplateenums 常量（enums 的值就是 i18n key）+ 一条**受控提示**
//	              前缀（shell.BulkIDs 的上限拒绝：整句由本仓库拼出、带可行动数字）；
//	② 归口文案 —— shell.MsgInternalError 词条（缺词条回落中文原文）；
//	③ 结构化日志 —— logger.Scene + user_id + 原始错误（原文只进日志）。

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

// contentTemplateErrScene 日志场景名（与 content_template_handle.go 的 shell.PageError 场景同源）。
const contentTemplateErrScene = "content_template"

// contentTemplateErrInternalFallback 归口文案的中文兜底（i18n 未初始化或该 key 没有词条时用）。
const contentTemplateErrInternalFallback = "系统内部错误，请稍后重试"

// contentTemplateFacingMessages 可以原样展示给运营的文案白名单（键 = contenttemplateenums 常量）。
//
// 这些 key 都是**运营能自己处理**的：模板不存在 / 被实例引用 / 文档格式非法 / 工程选错。
// 漏登记只会让页面少一句可读提示（一眼可见），而漏判的方向恰好相反 ——
// 内部字符串只要长得像 key 就会被透出。
var contentTemplateFacingMessages = map[string]string{
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

// contentTemplateControlledPrefixes 受控提示的前缀白名单。
//
// 目前只有一条：shell.BulkIDs 的上限拒绝「一次最多操作 N 项，当前 M 项，请分批进行」。
// 它由本仓库自己拼出、带可行动数字、不含库表信息。按**前缀**判而不是按来源直接透出：
// 上游将来改成上抛别的错误时前缀不再命中，自动退回归口文案。
var contentTemplateControlledPrefixes = []string{
	fmt.Sprintf("一次最多操作 %d 项", shell.MaxBulkIDs),
}

// contentTemplateControlledText 受控提示 → 原样透出；未命中返回空串。
func contentTemplateControlledText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, prefix := range contentTemplateControlledPrefixes {
		if strings.HasPrefix(raw, prefix) {
			return raw
		}
	}
	return ""
}

// contentTemplateInternalText 未命中任何白名单时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外只给归口文案。
func contentTemplateInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(contentTemplateErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "内容模板后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return shell.TranslateFor(c)(shell.MsgInternalError, contentTemplateErrInternalFallback)
}

// contentTemplateErrText 错误 → 当前语言的可展示文案。
//
// 命中白名单 → 原样（翻成当前语言），带参协议无关紧要 —— 这些 key 都是整句常量；
// 未命中 → 记日志 + 归口文案。
func contentTemplateErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	if msg := contentTemplateControlledText(raw); msg != "" {
		return msg
	}
	if msg, ok := contentTemplateFacingMessages[raw]; ok {
		return shell.TranslateFor(c)(msg, msg)
	}
	// service 会用 fmt.Errorf("%s: %w", enumsKey, err) 包装，取冒号前的 key 再查一次。
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if msg, ok := contentTemplateFacingMessages[strings.TrimSpace(raw[:idx])]; ok {
			return shell.TranslateFor(c)(msg, msg)
		}
	}
	return contentTemplateInternalText(c, err)
}

// sampleErrText 样例实体选取失败时的展示文案。
//
// hint 是**可行动提示**（依赖没装配 / 工程内还没有可预览的实体 / 不支持该实体类型），
// 原样可见；err 是依赖错误（products.List / contents.List 上抛，可能是 PG 原文）→
// 记日志 + 归口文案。两者分开是这一页的关键：样例实体要读两个别的模块的表，
// 那些错误里带着表名与 SQLSTATE，而这一页的提示位（?err= / SampleErr）都会被原样渲染。
func sampleErrText(c *gin.Context, hint string, err error) string {
	if h := strings.TrimSpace(hint); h != "" {
		return h
	}
	if err == nil {
		return ""
	}
	return contentTemplateInternalText(c, err)
}

// —— 读侧回执的收口（?err= / ?done=）——

// 本页自造的回执文案（不是 enums 白名单，也不来自 shell）：它们会进 ?err=，
// 因此必须同时登记在 contentTemplateLocalNotices 里 —— 自造文案不登记就会在回显时被自己吞掉。
const (
	contentTemplateMissingIDText      = "缺少模板 id"
	contentTemplateNotFoundText       = "模板不存在"
	contentTemplateSampleMissingText  = "缺少预览样例实体"
	contentTemplateHintProductNoMod   = "商品模块未装配，无法自动选取预览样例"
	contentTemplateHintContentNoMod   = "内容模块未装配，无法自动选取预览样例"
	contentTemplateHintNoProduct      = "工程内还没有商品，无法预览商品详情模板"
	contentTemplateHintNoArticle      = "工程内还没有文章，无法预览文章详情模板"
	contentTemplateHintEntityTypeMiss = "暂不支持该实体类型的预览样例自动选取（本页只支持商品与文章）"
	// 切换生效的成功回执（进 ?done=）。生效的那套换了意味着引用它的产物过期，
	// 回执把这件事说出来，免得有人以为「只是改了个标记」。
	contentTemplateActivateDoneText = "已切换生效模板，引用它的页面与实例会重新构建。"
	// 引用反查未装配时的提示。刻意**不写成「系统内部错误」**：正确读法是「查不出来」，
	// 而不是一次失败；而且它与「没有引用」必须长得不同 —— 后者会让人以为可以放心删。
	contentTemplateImpactUnavailableText = "引用反查能力未装配：本页无法列出引用这套模板的页面与实例，删除前请人工确认。"
)

// contentTemplateImpactUnparsableTemplate 影响面可能不完整的提示（%d = 无法解析的文档数）。
//
// 带占位符的受控文案（与批量结论同一形态），读侧经 shell.NoticeTemplate 归一后参与回显判定。
const contentTemplateImpactUnparsableTemplate = "有 %d 份文档无法解析，影响面可能不完整（这些文档仍可能引用本模板）。"

// contentTemplateLocalNotices 本页自造、可原样展示的回执文案。
var contentTemplateLocalNotices = []string{
	contentTemplatesNotReadyText,
	contentTemplateMissingIDText,
	contentTemplateNotFoundText,
	contentTemplateSampleMissingText,
	contentTemplateHintProductNoMod,
	contentTemplateHintContentNoMod,
	contentTemplateHintNoProduct,
	contentTemplateHintNoArticle,
	contentTemplateHintEntityTypeMiss,
	contentTemplateActivateDoneText,
	contentTemplateImpactUnavailableText,
}

// contentTemplatesBulkResultTemplates 批量删除的结论文案模板（%d 是计数字段）。
//
// **写侧与读侧共用这一份字面量**：写侧 contentTemplatesBulkDeleteResult 用它 Sprintf，
// 读侧 contentTemplateNoticeTexts 用它（经 shell.NoticeTemplate 归一）判定 URL 回显。
var contentTemplatesBulkResultTemplates = []string{
	"没有选中任何模板，列表未改动。",
	"已删除 %d 个模板。",
	"%d 个模板都未能删除，列表未改动（仍被页面、实例或其它模板引用的模板不能删除）。",
	"已删除 %d 个，%d 个未能删除（仍被页面、实例或其它模板引用的模板不能删除）。",
}

// contentTemplateNoticeTexts 本页可以原样展示的回执文案（当前语言）。
//
// 三类来源与写侧一一对应：① enums 白名单的展示文案（contentTemplateErrText 的产物）；
// ② 本页自造文案 + 归口文案 + shell 的批量上限提示；③ 批量结论文案模板。
func contentTemplateNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0,
		len(contentTemplateFacingMessages)+len(contentTemplateLocalNotices)+len(contentTemplatesBulkResultTemplates)+2)
	for _, msg := range contentTemplateFacingMessages {
		out = append(out, tr(msg, msg))
	}
	out = append(out, contentTemplateLocalNotices...)
	out = append(out,
		shell.BulkIDsNoticeTemplate(c),
		tr(shell.MsgInternalError, contentTemplateErrInternalFallback),
	)
	for _, tpl := range contentTemplatesBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	out = append(out, shell.NoticeTemplate(contentTemplateImpactUnparsableTemplate))
	return out
}

// contentTemplatePageErr 列表页 ?err= 的统一出口（未命中落归口文案）。
func contentTemplatePageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, contentTemplateNoticeTexts(c))
	})
}

// contentTemplatePageDone 列表页 ?done= 的统一出口（成功提示：未命中落空串）。
func contentTemplatePageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, contentTemplateNoticeTexts(c))
	})
}
