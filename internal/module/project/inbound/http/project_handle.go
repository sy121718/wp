package projecthttp

import (
	"errors"
	"net/http"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
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
		response.ErrorWithMessage(c, projectErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgProjectCreated, res)
}

// List 列出全部站点工程。
func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, err.Error())
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
		response.ErrorWithMessage(c, projectErrorStatus(err), err.Error())
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
		response.ErrorWithMessage(c, projectErrorStatus(err), err.Error())
		return
	}
	response.SuccessWithMessage(c, projectenums.MsgProjectUpdated, res)
}

func projectErrorStatus(err error) int {
	if errors.Is(err, errors.New(projectenums.ErrProjectNotFound)) || strings.Contains(err.Error(), projectenums.ErrProjectNotFound) {
		return http.StatusNotFound
	}
	if strings.Contains(err.Error(), projectenums.ErrInvalidName) || strings.Contains(err.Error(), projectenums.ErrInvalidSettings) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
