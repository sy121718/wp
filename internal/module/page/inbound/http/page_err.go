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

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

// pageErrScene 日志场景名（与 page 模块其它 logger.Scene("page") 一致）。
const pageErrScene = "page"

// pageErrControlledPrefixes 受控提示的前缀白名单。
//
// 它们不是 enums key（所以进不了 pageErrorStatus 的 sentinel 分类），但整句都由本仓库
// 自己拼出：不含表名 / SQLSTATE / 路径，且带着运营照着做的数字。目前只有一条 ——
// shell.BulkIDs 的上限拒绝「一次最多操作 N 项，当前 M 项，请分批进行」（internal/web/shell/bulk.go）。
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
// ErrPageNotFound → 「页面不存在」），所以这里翻一次再返回：?err= 与模板数据都是
// 直接渲染的文本，不经过 pkg/response 的翻译层 —— 不翻的话运营看到的是 ErrPageNotFound。
//
// 「key：明细」形态（service 用 fmt.Errorf("%s：…") 包过）保留明细，只翻 key 部分；
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
		text += "：" + tail
	}
	return text, true
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

// —— 读侧回执的收口（?err= / ?done=）——
//
// 页面管理页此前把 query 参数**原样**塞进模板数据（PagesList 的
// `Err: strings.TrimSpace(c.Query("err"))`）：写侧虽然已经受控，读侧仍是一条无界通道 ——
// 任何人手拼 /admin/pages?err=任意文案 就能在页面上塞一条顶着「上一次操作未完成」
// 样式的伪造消息。查询参数与响应体、模板数据一样**不是可信边界**。

// pagesBulkResultTemplates 批量删除的结论文案模板（%d 是计数字段）。
//
// **写侧与读侧共用这一份字面量**：写侧 pagesBulkDeleteResult 用它 Sprintf，
// 读侧 pageNoticeTexts 用它（经 shell.NoticeTemplate 归一）判定 URL 回显。
var pagesBulkResultTemplates = []string{
	"没有勾选任何页面，列表未改动。",
	"已删除 %d 个页面。",
	"%d 个页面都未能删除，列表未改动。",
	"已删除 %d 个，%d 个未能删除（可能已被删除或路径清理失败）。",
}

// pagesLocalNotices 页面管理页自造、可原样展示的回执文案。
const pagesLocalNoticeMissingID = "缺少页面 id，未执行删除。"

var pagesLocalNotices = []string{pagesLocalNoticeMissingID}

// pageFacingKeys 可以原样展示给运营的 page 业务错误 key。
//
// 取值与 pageErrorStatus（page_handle.go）逐条对应：那一份按 errors.Is 把 sentinel 分类成
// 400/404/409，default 才是 500 —— 这里列的正是**不是 500** 的那些。
// enums 的值就是常量名（也是 i18n key），key 形态与译文形态都被接受。
var pageFacingKeys = []string{
	pageenums.ErrInvalidParam,
	pageenums.ErrInvalidKind,
	pageenums.ErrInvalidDocument,
	pageenums.ErrInvalidPath,
	pageenums.ErrProjectRequired,
	pageenums.ErrPageNotFound,
	pageenums.ErrProjectNotFound,
	pageenums.ErrRollbackTargetMiss,
	pageenums.ErrDraftVersionConflict,
	pageenums.ErrPathOccupied,
	pageenums.ErrRebuildRequired,
	pageenums.ErrNoStagedArtifact,
}

// pageNoticeTexts 页面管理页可以原样展示的回执文案（当前语言）。
func pageNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, len(pageFacingKeys)*2+len(pagesLocalNotices)+len(pagesBulkResultTemplates)+3)
	for _, key := range pageFacingKeys {
		out = append(out, key, tr(key, key))
	}
	out = append(out,
		shell.PageInternalText(c),
		shell.BulkIDsNoticeTemplate(c),
	)
	out = append(out, pagesLocalNotices...)
	for _, tpl := range pagesBulkResultTemplates {
		out = append(out, shell.NoticeTemplate(tpl))
	}
	return out
}

// pagePageErr 页面管理页 ?err= 的统一出口（未命中落归口文案）。
func pagePageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, pageNoticeTexts(c))
	})
}

// pagePageDone 页面管理页 ?done= 的统一出口（成功提示：未命中落空串）。
func pagePageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, pageNoticeTexts(c))
	})
}
