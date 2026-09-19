// Package contenttemplatehttp contenttemplate 模块 HTTP 入口（0-A2）。
package contenttemplatehttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	"go_wp/pkg/response"
)

// Handle contenttemplate 接口处理器。
type Handle struct {
	svc contenttemplatecontract.ContentTemplateService
}

// NewHandle 构造。
func NewHandle(svc contenttemplatecontract.ContentTemplateService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建模板。
func (h *Handle) Create(c *gin.Context) {
	req := &contenttemplatedto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgCreateSuccess, res)
}

// Update 更新模板。
// Activate 切换生效模板（POST /api/contenttemplate/activate）。
func (h *Handle) Activate(c *gin.Context) {
	req := &contenttemplatedto.ActivateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Activate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) Update(c *gin.Context) {
	req := &contenttemplatedto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgUpdateSuccess, res)
}

// Get 模板详情。
func (h *Handle) Get(c *gin.Context) {
	req := &contenttemplatedto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgDetailSuccess, res)
}

// List 模板列表。
func (h *Handle) List(c *gin.Context) {
	req := &contenttemplatedto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contenttemplateenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "contenttemplate", err)
		return
	}
	response.SuccessWithMessage(c, contenttemplateenums.MsgListSuccess, list)
}
