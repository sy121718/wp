// comment_handle.go — comment 模块的 JSON 接口（BIZ-5）。
//
// 与后台页面（comment_page_handle.go）的分工：
//
//	· 页面：表单 POST → 302 回列表（?err= / ?done= 回带），数据由 templateMap 组装；
//	· 接口：走 pkg/response 的 JSON 信封（authorizedAPI 三层链：Session + CSRF + Casbin）。
//
// 两者共用同一份 service 与同一份错误归口（comment_err.go），所以「同一个错误在页面与
// 接口上说法一致」不是靠两边各写一遍。
//
// 为什么这两条接口**必须存在**（哪怕页面已经能做同样的事）：权限点的真源是
// **接口注册处**的声明动作（permission.RouteGroup.GET/POST 的第二个参数），
// 权限点在此登记、装配末尾幂等 upsert 进 sys_permission；页面的写动作经
// builtin.CasbinMiddlewareForPath("/api/comment/review") **复用**这条接口的权限点。
// 少一条接口 = 少一个权限点 = 含超管在内全员 403。
package commenthttp

import (
	"github.com/gin-gonic/gin"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/internal/web/shell"
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
// 审核人取自**会话**（shell.CurrentUserID），不从请求体读：让请求方能填操作人
// 等于把审计留痕变成可伪造字段。
func (h *Handle) Review(c *gin.Context) {
	req := &commentdto.ReviewReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ParamError(c)
		return
	}
	req.ReviewerID = shell.CurrentUserID(c)
	res, err := h.svc.Review(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, commentErrStatus(err), commentErrText(c, err))
		return
	}
	// 回执带条数：调用方据此区分「改了这一批」与「一条都没变」（并发的另一次审核
	// 已经把它改成目标状态）—— 两者都不是失败，但必须看得见差别。
	response.SuccessWithMessage(c, reviewMessageKey(res), res)
}

// reviewMessageKey 批量审核的成功回执 key（按目标状态区分）。
func reviewMessageKey(res *commentdto.ReviewResp) string {
	if res != nil && res.Status == commentenums.StatusRejected {
		return commentenums.MsgReviewRejected
	}
	return commentenums.MsgReviewApproved
}
