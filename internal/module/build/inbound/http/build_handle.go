// Package buildhttp build 模块 HTTP 接入层（构建任务队列，审计 DB-007）。
package buildhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	"go_wp/pkg/response"
)

// Handle build 模块处理器。
type Handle struct {
	svc buildcontract.BuildService
}

// NewHandle 创建处理器。
func NewHandle(svc buildcontract.BuildService) *Handle { return &Handle{svc: svc} }

// Queue 队列状态（GET /api/build/queue）。
//
// 后台可见性是这条队列存在的理由之一：构建失败若只留在日志里，
// 运营看到的现象是「网站没更新」而查不到原因。
func (h *Handle) Queue(c *gin.Context) {
	res, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		response.ErrorInternal(c, "build", err)
		return
	}
	response.SuccessWithMessage(c, buildenums.MsgQueueFound, res)
}

// List 任务列表（GET /api/build/jobs?status=&limit=）。
func (h *Handle) List(c *gin.Context) {
	var req builddto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, buildenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		response.ErrorInternal(c, "build", err)
		return
	}
	response.Success(c, list)
}

// retryReq 重试请求。
type retryReq struct {
	ID string `json:"id"`
}

// Retry 重试失败任务（POST /api/build/retry）。
func (h *Handle) Retry(c *gin.Context) {
	var req retryReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		response.ParamError(c, buildenums.ErrInvalidParam)
		return
	}
	// 只把业务错误原样回传（例如任务不存在 / 不是失败态）；内部错误不外泄细节。
	if err := h.svc.Retry(c.Request.Context(), req.ID); err != nil {
		if err.Error() == buildenums.ErrJobNotFound || err.Error() == buildenums.ErrInvalidParam {
			response.ErrorAuto(c, http.StatusBadRequest, "build", err)
			return
		}
		response.ErrorInternal(c, "build", err)
		return
	}
	response.SuccessWithMessage(c, buildenums.MsgJobRetried, nil)
}
