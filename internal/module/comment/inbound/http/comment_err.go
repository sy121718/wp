// comment_err.go — comment 模块的错误响应归口（错误文案三件套的读侧）。
//
// 三件套（AGENTS.md §响应与错误处理）：
//
//	① 白名单 —— enums.CommentFacingMessages，与 enums 常量一一对应（AST 对账测试钉住）；
//	② 归口文案 —— 未命中时返回 enums.ErrInternal（可翻译 key）；
//	③ 结构化日志 —— 原文只进日志，带场景与 user_id。
//
// 为什么必须收口：service 一旦把 PostgreSQL 原文上抛（表名 / 索引名 / SQLSTATE），
// 它会随 500 原样直出给前端。响应不是可信边界（后台不是，访客页面更不是）。
// 门禁：scripts/check-no-internal-error-leak.sh。
//
// 形态与 membership_err.go 同源，差别只在模块前缀与「不做定位信息拼接」——
// 评论的业务错误不带定位数据（哪一条被拒是日志的事，页面上不需要逐条回带），
// 所以这里没有 SplitFacingDetail / facingSep 那一套。
package commenthttp

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// commentErrScene 日志场景名（与模块内其它 logger.Scene("comment") 一致）。
const commentErrScene = "comment"

// commentFacingText 命中白名单 → 返回可对外文案（key 原文）；未命中 → ("", false)。
//
// **不记日志**：给页面路径用（那里已在调用点记了更具体的一条，避免同一个错误记两次）。
func commentFacingText(err error) (string, bool) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return commentenums.HitFacingMessage(msg)
}

// commentErrText JSON 接口的错误文案出口。
//
// 命中白名单 → 业务文案（按请求语言取词）；未命中 → 记结构化日志 + 归口文案。
func commentErrText(c *gin.Context, err error) string {
	if msg, ok := commentFacingText(err); ok {
		return response.TranslateMessage(c, msg)
	}
	logger.Scene(commentErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "comment 接口内部错误")
	return commentenums.ErrInternal
}

// commentErrPageText 后台页面路径的归口文案（整页渲染 + 提示条，不走 pkg/response）。
//
// 与 JSON 出口分开的理由同 membership：页面把文案拼进提示条，泄漏的是**页面正文**，
// 而 check-no-internal-error-leak.sh 的判据只看 response.ErrorWithMessage / c.String 的实参。
func commentErrPageText(c *gin.Context, err error) string {
	if msg, ok := commentFacingText(err); ok {
		return shell.TranslateFor(c)(msg, msg)
	}
	logger.Scene(commentErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "comment 后台页面内部错误")
	return shell.PageInternalText(c)
}

// commentErrStatus 把业务错误映射到 HTTP 状态码。
//
// 判据只看业务 key（与白名单同一份读法）：503 未接入 → 503；其余可展示业务错误 → 400；
// 未命中（内部错误）→ 500。
func commentErrStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch err.Error() {
	case commentenums.ErrUnavailable:
		return http.StatusServiceUnavailable
	case commentenums.ErrInvalidParam, commentenums.ErrProjectRequired,
		commentenums.ErrEntityTypeUnknown, commentenums.ErrEntityIDInvalid,
		commentenums.ErrBodyRequired, commentenums.ErrBodyTooLong,
		commentenums.ErrRateLimited, commentenums.ErrNotAllowed,
		commentenums.ErrLoginRequired, commentenums.ErrCSRFRequired,
		commentenums.ErrNothingSelected, commentenums.ErrParentInvalid:
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// commentNoticeTexts 本模块页面上可以原样展示的回执文案（当前语言）。
//
// 写侧（?err= / ?done= 回带）与读侧共用这一份：读侧按形状**整体**匹配，
// 未命中的 query 参数会被当伪造文案丢掉（查询参数不是可信边界）。
func commentNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, (len(commentenums.CommentFacingMessages)+2)*2)
	for _, key := range commentenums.CommentFacingMessages {
		out = append(out, key, tr(key, key))
	}
	out = append(out,
		tr(commentenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）"),
		shell.BulkIDsNoticeTemplate(c),
	)
	return out
}

// commentPageErr 列表页 ?err= 的统一出口（未命中落归口文案）。
func commentPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, commentNoticeTexts(c))
	})
}

// commentPageDone 列表页 ?done= 的统一出口（成功提示：未命中落空串）。
func commentPageDone(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("done"), "", func(raw string) string {
		return shell.FacingNotice(raw, commentNoticeTexts(c))
	})
}

// commentErrURL 页面写失败的回跳地址（带 ?err= 业务文案）。
//
// 回带的是**已过白名单的成品文案**（commentErrPageText），不是 err.Error()：
// 内部错误因此只以归口文案出现在 URL 里，原文留在日志。
func commentErrURL(c *gin.Context, f commentFilterValues, err error) string {
	return commentErrURLText(f, commentErrPageText(c, err))
}

// commentErrURLText 同上，但文案已由调用点备好（如 shell.BulkIDsFacingText 的成品文案）。
func commentErrURLText(f commentFilterValues, text string) string {
	q := filterQuery(f)
	q.Set("err", strings.TrimSpace(text))
	return commentsPath + "?" + q.Encode()
}

// commentOKURL 页面写成功的回跳地址（带 ?done= 成品回执）。
//
// 筛选条件**整组保留**：审核完要回到「同一个视图」继续处理下一条，
// 被弹回「全部状态、第 1 页」会让人每处理一批就得重新点一遍筛选。
func commentOKURL(f commentFilterValues, notice string) string {
	q := filterQuery(f)
	if notice != "" {
		q.Set("done", notice)
	}
	if len(q) == 0 {
		return commentsPath
	}
	return commentsPath + "?" + q.Encode()
}

// filterQuery 把当前筛选条件转成查询参数（回跳与分页共用同一份口径）。
func filterQuery(f commentFilterValues) url.Values {
	q := url.Values{}
	if f.Project != "" {
		q.Set("project", f.Project)
	}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.EntityType != "" {
		q.Set("entityType", f.EntityType)
	}
	if f.Keyword != "" {
		q.Set("keyword", f.Keyword)
	}
	return q
}

// errCommentParam 参数级校验失败（判定就在 handle 里，没有 error 对象）。
//
// 与 membership 的同名助手同一理由：参数级失败也要落在白名单的候选里，
// 否则读侧会把回带的文案当伪造文案丢掉（页面上什么都不显示，也没有任何日志）。
func errCommentParam() error { return errors.New(commentenums.ErrInvalidParam) }
