// Package producthttp product 模块 HTTP 入口（issue #5 / T3a）。
package producthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/response"
)

// Handle product 接口处理器。
type Handle struct {
	svc productcontract.ProductService
}

// NewHandle 构造。
func NewHandle(svc productcontract.ProductService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建商品（自动生成首个变体）。
func (h *Handle) Create(c *gin.Context) {
	req := &productdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// Update 修改商品。
func (h *Handle) Update(c *gin.Context) {
	req := &productdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// Get 商品详情（含变体与价格区间）。
func (h *Handle) Get(c *gin.Context) {
	req := &productdto.GetReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// List 商品列表。
func (h *Handle) List(c *gin.Context) {
	req := &productdto.ListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// Delete 删除商品。
func (h *Handle) Delete(c *gin.Context) {
	req := &productdto.DeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// CreateVariant 新增变体（未填字段由商品级默认值补齐）。
func (h *Handle) CreateVariant(c *gin.Context) {
	req := &productdto.CreateVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateVariant 修改变体（编辑路径不做默认值填充）。
func (h *Handle) UpdateVariant(c *gin.Context) {
	req := &productdto.UpdateVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// DeleteVariant 删除变体。
func (h *Handle) DeleteVariant(c *gin.Context) {
	req := &productdto.DeleteVariantReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteVariant(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// 编辑路径的默认值语义由 service 保证：本层只做绑定与转发，不补字段。
var _ = http.StatusOK
