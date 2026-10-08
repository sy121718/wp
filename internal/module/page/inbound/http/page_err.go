package pagehttp

// page_err.go — page 后台页的错误文案归口（第三波 CQ-009 形态 ②③ 收口）。
//
// 背景：page 模块的 JSON 出口早有 pageErrorMessage（业务 sentinel 原文 / 其余兜底 + 日志），
// 但**页面路径**是分批长出来的 —— `c.Redirect(..., pagesBackURL(berr.Error(), ""))` 这类写法
// 把 err.Error() 直接塞进 ?err=，而页面会把它原样渲染出来（admin/pages.html：
// `{{.Err}}`）；page_translations 的逐行校验错误同理（模板数据 `data.Errors`）。
// 两者与 JSON body 一样不是可信边界：service 一旦把 PostgreSQL 原文上抛（表名、
// SQLSTATE 42P01、约束名），它就会出现在运营眼前的页面上。
//
// 三件套（与 admin 的 admin_err.go / navigation 的 navigation_err.go 同形）：
//
//	① 白名单   —— pageErrorStatus 认得的 pageservice sentinel（errors.Is 精确分类，
//	              default 落到 500 即「不是本模块认识的业务错误」）；再加一条**受控提示**
//	              前缀（shell.BulkIDs 的上限拒绝：整句由本仓库拼出、带可行动数字）；
//	② 归口文案 —— shell.PageInternalText(c)（词条键 MsgInternalError，缺词条回落中文原文）；
//	③ 结构化日志 —— logger.Scene("page") + user_id + 原始错误（原文只进日志）。
//
// **传输通道已换**：写动作的结论由 shell.RenderJump 渲染成整页提示（见 page_jump.go），
// 不再经 302/303 + `?err=` / `?done=` 回带列表页。读侧那套判定（pagePageErr /
// pagePageDone / pageNoticeTexts / pageFacingKeys / pagesLocalNotices）随之整批删除 ——
// 查询参数不是可信边界，文案走响应体之后就不需要再证明「这条提示出自本仓」。
// 本文件只剩**写侧出口的归口**：命中白名单给业务文案，未命中落归口文案 + 日志。

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// pageErrScene 日志场景名（与 page 模块其它 logger.Scene("page") 一致）。
const pageErrScene = "page"

// pageErrControlledPrefixes 受控提示的前缀白名单。
//
// 它们不是 enums key（所以进不了 pageErrorStatus 的 sentinel 分类），但整句都由本仓库
// 自己拼出：不含表名 / SQLSTATE / 路径，且带着运营照着做的数字。目前只有一条 ——
// shell.BulkIDs 的上限拒绝「一次最多操作 N 项，当前 M 项，请分批进行」（internal/shell/bulk.go）。
//
// 按**前缀**判而不是按来源直接透出：来源受控这件事会随上游改变。前缀不再命中时
// 自动退回「记日志 + 归口文案」，不会把不认识的原文顺出去。
var pageErrControlledPrefixes = []string{
	fmt.Sprintf("一次最多操作 %d 项", shell.MaxBulkIDs),
}

// pageControlledText 受控提示 → 原样透出（保留可行动信息）；未命中返回空串。
func pageControlledText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, prefix := range pageErrControlledPrefixes {
		if strings.HasPrefix(raw, prefix) {
			return raw
		}
	}
	return ""
}

// pageFacingText 命中可展示文案 → (文案, true)；未命中 → ("", false)。
//
// 判据是 pageErrorStatus：它逐个 errors.Is 分类 pageservice 的 sentinel，default 落到 500。
// 业务错误的取值就是 pageenums 常量，而 **enums 的值即 i18n key**（迁移 058 已 seed
// ErrPageNotFound → 「页面不存在」），所以这里翻一次再返回：提示页与 JSON 出口都是
// 直接渲染的文本，不经过 pkg/response 的翻译层 —— 不翻的话运营看到的是 ErrPageNotFound。
//
// 「key：明细」形态（service 用 fmt.Errorf("%w: <ErrorDetail 编码>") 包过）只翻能识别的部分：
// key 取词条，明细按 i18n.ErrorDetail 协议取词并填 {name}；**明细不是词条就丢弃并落日志** ——
// 改造前这里是 `text += "：" + tail`（原样透出 builder 的中文原文），英文界面上必然中英混排。
// 取不到词条时 TranslateFunc 回落 key 本身（pkg/i18n 的兜底链），不输出空串。
func pageFacingText(c *gin.Context, err error) (string, bool) {
	if err == nil {
		return "", false
	}
	raw := strings.TrimSpace(err.Error())
	if msg := pageControlledText(raw); msg != "" {
		return msg, true
	}
	if pageErrorStatus(err) == http.StatusInternalServerError {
		return "", false
	}
	key, tail := raw, ""
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		key, tail = strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:])
	}
	text := shell.TranslateFor(c)(key, key)
	if tail != "" {
		if detail := pageDetailText(shell.TranslateFor(c), tail); detail != "" {
			text += "：" + detail
		}
	}
	return text, true
}

// pageDetailText 业务错误的**补充说明** → 当前语言文案（不是词条时返回空串）。
//
// 接受的形态只有一种：i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，
// 可多段）。其余一律**丢弃并落日志** —— 这与 product / inventory 的读侧口径有意不同：
// 那两处的 `return tail` 是为了兼容改造前就已存在的纯文本明细，而 page 这一族从本批起
// 全部改走 ErrorDetail（写侧见 page/service/page_document_detail.go），所以不再保留
// 「原样透出」这条无界通道 —— 它正是英文界面上中文混排的来源。
//
// 未登记的明细 key（拼错 / 新加漏登记）跳过该段并记一条日志：少一句补充说明，
// 好过把编码串或半截占位符摆到页面上。
func pageDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		logger.Scene(pageErrScene).With("tail", i18n.DetailTailForLog(tail)).
			Warn("业务错误的补充说明不是登记的词条形态，已丢弃（不再原样透出）")
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := pageenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(pageErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

// pageFacingOrInternal 页面路径的文案出口（**不记日志**）。
//
// 给「调用点已经记过一条更具体的日志（带 pageId / lang）」的地方用 ——
// 再记一次会让同一个错误在日志里出现两遍（navigation_err.go 出于同一理由拆出了
// 不记日志的 navigationFacingText）。
func pageFacingOrInternal(c *gin.Context, err error) string {
	if text, ok := pageFacingText(c, err); ok {
		return text
	}
	return shell.PageInternalText(c)
}

// pageErrPageText 页面路径错误文案的统一出口（记日志）。
//
// 未命中白名单时：原文只进日志（场景 + user_id + 原始错误），对外给归口文案。
func pageErrPageText(c *gin.Context, err error) string {
	if text, ok := pageFacingText(c, err); ok {
		return text
	}
	if err != nil {
		logger.Scene(pageErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "page 后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return shell.PageInternalText(c)
}

// pageTranslationSentinels 译文逐行校验（builder.ValidateContentTarget）的 sentinel。
//
// 它们是**纯校验**（不碰库、不碰文件）的固定中文提示，且带字段名 / 字节数等明细，
// 属于可直接展示给编辑者的业务文案 —— 命中即原样透出。
var pageTranslationSentinels = []error{
	builder.ErrContentContextInvalid,
	builder.ErrContentSourceSkipped,
	builder.ErrContentTargetEmpty,
	builder.ErrContentTargetTooLong,
	builder.ErrContentTargetShape,
}

// pageTranslationRowText 译文逐行校验错误 → 可展示文案。
//
// 命中 builder 的 5 个 sentinel → 原样（含「上限 N 字节，实际 M 字节」这类明细）；
// 未命中（将来若混进依赖错误）→ 记日志 + 归口文案。
func pageTranslationRowText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	for _, sentinel := range pageTranslationSentinels {
		if errors.Is(err, sentinel) {
			return err.Error()
		}
	}
	return pageErrPageText(c, err)
}

// pageBulkText 批量结论文案的一条模板 / 自造回执（i18n key + 中文原文）。
//
// **key 与中文原文只有这一份**：写侧 pagesBulkDeleteResult / 单条删除路径 / 重定向批量
// 拿它 Sprintf 出整句（经 shell.RenderJump 直接渲染进响应体）。
type pageBulkText struct{ key, fallback string }

// pageBulkTextOf 取一条批量结论文案的当前语言文本。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（本批的模板一律只允许 %s，
// 数字先经 strconv.Itoa）—— 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func pageBulkTextOf(c *gin.Context, t pageBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// pagesBulkResultTemplates 批量删除的结论文案模板（%s 是计数字段）。
//
// 写侧 pagesBulkDeleteResult 用它 Sprintf 出整句（经 shell.RenderJump 渲染）。
var pagesBulkResultTemplates = []pageBulkText{
	{pageenums.BulkPageNoneSelected, "没有勾选任何页面，列表未改动。"},
	{pageenums.BulkPageAllDeleted, "已删除 %s 个页面。"},
	{pageenums.BulkPageAllSkipped, "%s 个页面都未能删除，列表未改动。"},
	{pageenums.BulkPagePartial, "已删除 %s 个，%s 个未能删除（可能已被删除或路径清理失败）。"},
}

// 页面管理页自造、可原样展示的回执文案（经 shell.RenderJump 渲染进整页提示）。
var (
	pagesLocalNoticeMissingID = pageBulkText{pageenums.BulkPageMissingID, "缺少页面 id，未执行删除。"}
	// pagesLocalNoticeMissingPageID 翻译工作台保存时缺页面 id
	// （POST /admin/pages/translations/save 的 pageId 为空）。
	//
	// 该分支原先 `c.String(400, pageenums.MsgFieldRequired)`：响应体只有 i18n 的 **key 本身**
	// （16 字节的 "MsgFieldRequired"）—— 用户看到的是内部标识符，而不是给运营看的文案，
	// 而且脱离页壳。key 复用通用必填词条（词条值就是「必填字段不能为空」），
	// 结论由 shell.RenderJump 渲染成整页提示（见 page_jump.go）。
	pagesLocalNoticeMissingPageID = pageBulkText{pageenums.MsgFieldRequired, "必填字段不能为空，未保存。"}

	// pagesLocalNoticeProjectNameRequired 新建站点工程时名称为空
	// （POST /admin/projects/create 的 name 为空）。
	//
	// 原先这里是 `c.String(400, "项目名称不能为空")`：浏览器里只剩一行纯文本，
	// 侧栏 / 页头 / 抽屉 / 用户刚填的内容全没了（AGENTS.md 形态 ①）。改成整页提示
	// （shell.RenderJump）回带原因。
	//
	// 为什么不复用通用词条 MsgFieldRequired：这条回执要说清「哪一条没填」。
	// 通用句「必填字段不能为空」在页面上等于什么都没说 —— 用户要自己把抽屉再开一遍才知道。
	pagesLocalNoticeProjectNameRequired = pageBulkText{pageenums.PageFormProjectNameRequired, "站点工程名称不能为空，未创建。"}
	// pagesLocalNoticePathRequired 新建页面时路径为空（POST /admin/pages/create 的 draftPath 为空）。
	//
	// 缺工程 id **不走**这一条：那里复用 pageenums.ErrProjectRequired（403 已登记中英词条，
	// 语义就是「没有工程作用域」）。两条分开报的判据与 project 域主题页一致 ——
	// 合成句「项目与页面路径不能为空」把两件事说成同一件事，而修法完全不同：
	// 缺工程是选择器 / 工程列表的问题，缺路径是输入框的问题。
	pagesLocalNoticePathRequired = pageBulkText{pageenums.PageFormPathRequired, "页面路径不能为空，未创建。"}
)

// pageFacingKey 取一条**本域 enums 文案 key** 的当前语言文本（handler 侧已知它可展示时用）。
//
// 与 pageFacingText 的分工：那里的入参是 error（来源要在运行期判定、未命中要记日志），
// 这里入参是本域 enums 常量（来源在编译期已确定），所以只取词、不判定、不记日志。
//
// fallback 给 key 本身：缺词条的页面会显示裸 key（一眼可见），而给空串会让
// 「操作失败」在页面上**完全不可见** —— 用户以为操作成功了。
func pageFacingKey(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, key)
}
