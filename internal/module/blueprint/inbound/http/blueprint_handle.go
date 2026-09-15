// Package blueprinthttp blueprint 模块 HTTP 入口（0-B）。
package blueprinthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintenums "go_wp/internal/module/blueprint/enums"
	"go_wp/pkg/response"
)

// Handle blueprint 接口处理器。
type Handle struct {
	svc blueprintcontract.BlueprintService
}

// NewHandle 构造。
func NewHandle(svc blueprintcontract.BlueprintService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建 Blueprint。
func (h *Handle) Create(c *gin.Context) {
	req := &blueprintdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgCreateSuccess, res)
}

// Update 更新 Blueprint。
func (h *Handle) Update(c *gin.Context) {
	req := &blueprintdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgUpdateSuccess, res)
}

// Publish 发布 Blueprint。
func (h *Handle) Publish(c *gin.Context) {
	req := &blueprintdto.PublishReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgPublishSuccess, res)
}

// Get Blueprint 详情。
func (h *Handle) Get(c *gin.Context) {
	req := &blueprintdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgDetailSuccess, res)
}

// List Blueprint 列表。
func (h *Handle) List(c *gin.Context) {
	req := &blueprintdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgListSuccess, list)
}

// Delete 删除 Blueprint。
func (h *Handle) Delete(c *gin.Context) {
	req := &blueprintdto.DeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgDeleteSuccess, nil)
}

// Init 初始化 Page Document（复制 AST 并递归重写 Node ID）。
func (h *Handle) Init(c *gin.Context) {
	req := &blueprintdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, blueprintenums.ErrInvalidParam)
		return
	}
	doc, err := h.svc.InitPageDocument(c.Request.Context(), req.ID)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.Success(c, doc)
}
