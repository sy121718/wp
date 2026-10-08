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
//
// **读侧（?err= / ?done= 白名单与回跳 URL）已整批删除**：写动作的结论由 shell.RenderJump
// 渲染成提示页（文案走响应体），回跳地址由 shell.BackPath 从表单 action 的 query 读回 ——
// 查询参数不再承载文案，那套「证明这条提示出自本仓」的判定随之不需要了。
package commenthttp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/internal/shell"
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

// errCommentParam 参数级校验失败（判定就在 handle 里，没有 error 对象）。
//
// 与 membership 的同名助手同一理由：参数级失败也要落在白名单的候选里，
// 否则读侧会把回带的文案当伪造文案丢掉（页面上什么都不显示，也没有任何日志）。
func errCommentParam() error { return errors.New(commentenums.ErrInvalidParam) }
