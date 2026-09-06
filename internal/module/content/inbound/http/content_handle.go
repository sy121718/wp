// Package contenthttp content 模块 HTTP 入口（0-A2）。
package contenthttp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	"go_wp/pkg/response"
)

// Handle content 接口处理器。
type Handle struct {
	svc contentcontract.ContentService
}

// NewHandle 构造。
func NewHandle(svc contentcontract.ContentService) *Handle { return &Handle{svc: svc} }

// Create 新建内容。
func (h *Handle) Create(c *gin.Context) {
	req := &contentdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgCreateSuccess, res)
}

// Update 更新内容。
func (h *Handle) Update(c *gin.Context) {
	req := &contentdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgUpdateSuccess, res)
}

// Get 内容详情。
func (h *Handle) Get(c *gin.Context) {
	req := &contentdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDetailSuccess, res)
}

// List 内容列表。
func (h *Handle) List(c *gin.Context) {
	req := &contentdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgListSuccess, list)
}

// Delete 删除内容。
func (h *Handle) Delete(c *gin.Context) {
	req := &contentdto.DeleteReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, contentenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, contentenums.MsgDeleteSuccess, nil)
}

// 保留 errors 引用位（ShouldBind 错误细分预留）。
var _ = errors.Is
