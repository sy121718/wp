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

// Queue 队列状态（GET /api/build/queue?project=）。
//
// 后台可见性是这条队列存在的理由之一：构建失败若只留在日志里，
// 运营看到的现象是「网站没更新」而查不到原因。
//
// project 是可选的工程过滤：build_jobs **没有** RLS 策略（全库唯一的例外，见
// build/model 包注释），换非超级业务角色后不带过滤的查询不会 fail closed，
// 而是把别的工程的队列一起列出来。不带 project 时保持原有的全队列视角。
func (h *Handle) Queue(c *gin.Context) {
	res, err := h.svc.Stats(c.Request.Context(), strings.TrimSpace(c.Query("project")))
	if err != nil {
		response.ErrorInternal(c, "build", err)
		return
	}
	response.SuccessWithMessage(c, buildenums.MsgQueueFound, res)
}

// List 任务列表（GET /api/build/jobs?project=&status=&limit=）。
//
// project 可选：不带它时是全队列视角（运维排查用），带上就只看该工程的任务 ——
// 这条是本表唯一的工程过滤入口（build_jobs 没有 RLS 策略，见 build/model 包注释）。
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
	// ProjectID 可选：给出时只允许退回该工程的任务（build_jobs 没有 RLS 策略，
	// 否则任何一个工程的失败任务都能被别的工程的重试请求改回 pending）。
	ProjectID string `json:"projectId"`
}

// Retry 重试失败任务（POST /api/build/retry）。
func (h *Handle) Retry(c *gin.Context) {
	var req retryReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.ID) == "" {
		response.ParamError(c, buildenums.ErrInvalidParam)
		return
	}
	// 只把业务错误原样回传（例如任务不存在 / 不是失败态）；内部错误不外泄细节。
	if err := h.svc.Retry(c.Request.Context(), strings.TrimSpace(req.ProjectID), req.ID); err != nil {
		if err.Error() == buildenums.ErrJobNotFound || err.Error() == buildenums.ErrInvalidParam {
			response.ErrorAuto(c, http.StatusBadRequest, "build", err)
			return
		}
		response.ErrorInternal(c, "build", err)
		return
	}
	response.SuccessWithMessage(c, buildenums.MsgJobRetried, nil)
}
