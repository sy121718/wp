// Package presentationhttp presentation 模块 HTTP 入口（0-A2）。
package presentationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/response"
)

// Handle presentation 接口处理器。
type Handle struct {
	svc presentationcontract.PresentationService
}

// NewHandle 构造。
func NewHandle(svc presentationcontract.PresentationService) *Handle { return &Handle{svc: svc} }

// CreateInstance 创建自动发布实例。
func (h *Handle) CreateInstance(c *gin.Context) {
	req := &presentationdto.CreateInstanceReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateInstance(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgCreateSuccess, res)
}

// Rebuild 实体更新后重建。
func (h *Handle) Rebuild(c *gin.Context) {
	req := &presentationdto.RebuildReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Rebuild(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgRebuildSuccess, res)
}

// Get 实例详情。
func (h *Handle) Get(c *gin.Context) {
	req := &presentationdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDetailSuccess, res)
}

// List 实例列表。
func (h *Handle) List(c *gin.Context) {
	req := &presentationdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgListSuccess, list)
}

// Delete 删除实例。
func (h *Handle) Delete(c *gin.Context) {
	req := &presentationdto.DeleteReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, presentationenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, presentationenums.MsgDeleteSuccess, nil)
}
