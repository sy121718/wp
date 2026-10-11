// comment_handle.go — comment 模块的 JSON 接口（BIZ-5）。
//
// 与后台页面（comment_page.go）的分工：
//
//	· 页面：GET 展示审核队列，数据由 templateMap 组装；
//	· 接口：走 pkg/response 的 JSON 信封（authorizedAPI 三层链：Session + CSRF + Casbin）。
//
// 两者共用同一份 service 与同一份错误归口（comment_err.go），所以「同一个错误在页面与
// 接口上说法一致」不是靠两边各写一遍。
//
// 接口注册处声明权限点；浏览器审核直调本接口。
package commenthttp

import (
	"net/http"
	"strconv"
	"strings"

	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/response"
)

// Handle comment 模块的 JSON 接口处理器。
type Handle struct {
	svc commentcontract.CommentService
}

// NewHandle 构造。
func NewHandle(svc commentcontract.CommentService) *Handle { return &Handle{svc: svc} }

// AdminList 审核队列（分页 + 状态 / 实体类型 / 关键词筛选）。
func (h *Handle) AdminList(c *gin.Context) {
	req := &commentdto.AdminListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.AdminList(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, commentErrStatus(err), commentErrText(c, err))
		return
	}
	response.Success(c, res)
}

// Review 批量通过 / 驳回。
//
// 审核人取自**会话**（builtin.CurrentUserID），不从请求体读：让请求方能填操作人
// 等于把审计留痕变成可伪造字段。
func (h *Handle) Review(c *gin.Context) {
	req := &commentdto.ReviewReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ParamError(c)
		return
	}
	req.ReviewerID = builtin.CurrentUserID(c)
	res, err := h.svc.Review(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, commentErrStatus(err), commentErrText(c, err))
		return
	}
	// 回执带服务端实际受影响条数；零行同样是成功回执，不推断未匹配的原因。
	c.JSON(http.StatusOK, response.Response{
		Code: http.StatusOK, Message: commentReviewMessage(c, res), Data: res,
	})
}

// commentReviewMessage 只填后端确认的受影响条数；词条限定为单个 %d。
func commentReviewMessage(c *gin.Context, res *commentdto.ReviewResp) string {
	pair := commentenums.DoneTemplate(res != nil && res.Status == commentenums.StatusApproved)
	tpl := i18n.TranslateFunc(response.RequestLanguage(c))(pair.Key, pair.Fallback)
	if strings.Count(tpl, "%d") != 1 || strings.Contains(strings.Replace(tpl, "%d", "", 1), "%") {
		tpl = pair.Fallback
	}
	var affected int64
	if res != nil {
		affected = res.Affected
	}
	return strings.Replace(tpl, "%d", strconv.FormatInt(affected, 10), 1)
}
