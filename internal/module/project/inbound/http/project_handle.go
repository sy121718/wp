package projecthttp

import (
	"errors"
	"net/http"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle project 模块 HTTP 处理器。
type Handle struct {
	svc projectcontract.ProjectService
}

// NewHandle 创建 project HTTP 处理器。
func NewHandle(svc projectcontract.ProjectService) *Handle {
	return &Handle{svc: svc}
}

// Create 创建站点工程。
func (h *Handle) Create(c *gin.Context) {
	var req projectdto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, projectenums.ErrInvalidName)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		projectError(c, err)
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgProjectCreated, res)
}

// List 列出全部站点工程。
func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context())
	if err != nil {
		projectError(c, err)
		return
	}
	response.Success(c, res)
}

// Detail 查询站点工程。
func (h *Handle) Detail(c *gin.Context) {
	var req projectdto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		projectError(c, err)
		return
	}
	response.Success(c, res)
}

// Update 更新站点工程与 SiteSettings。
func (h *Handle) Update(c *gin.Context) {
	var req projectdto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), &req)
	if err != nil {
		projectError(c, err)
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgProjectUpdated, res)
}

// projectError 将工程业务错误映射为响应状态码与文案：
// 业务哨兵 → 对应 enums 文案；其余（基础设施故障）→ 兜底文案 + 日志留原文，
// 不向客户端泄漏内部错误细节。
func projectError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, projectenums.ErrProjectInternal
	switch {
	case errors.Is(err, projectservice.ErrProjectNotFound):
		status, message = http.StatusNotFound, projectenums.ErrProjectNotFound
	case errors.Is(err, projectservice.ErrInvalidName),
		errors.Is(err, projectservice.ErrInvalidSettings),
		errors.Is(err, projectservice.ErrInvalidParam):
		status, message = http.StatusBadRequest, err.Error()
	default:
		logger.Scene("project").Error(err, "工程操作失败")
	}
	response.ErrorWithMessage(c, status, message)
}
