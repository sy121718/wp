package producthttp

import (
	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

func (h *ProductPageHandle) ProductAttributesPage(c *gin.Context) {
	req, ok := binding.Query[productdto.CatalogTaxonomyReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	req.ProjectID = scope.ProjectID
	result, err := h.products.QueryAttributes(c.Request.Context(), req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}

	pageview.Render(c, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req), pageview.Object("Result", result)))
}

// CreateAttribute 新建属性组（可同时带初始属性值）。
func (h *ProductHandle) CreateAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateAttribute 修改属性组本身（值走 SetAttributeValues）。
func (h *ProductHandle) UpdateAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// SetAttributeValues 整体保存属性值（全量替换：请求里没有的值被删除）。
func (h *ProductHandle) SetAttributeValues(c *gin.Context) {
	req, ok := binding.JSON[productdto.SetAttributeValuesReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SetAttributeValues(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetAttribute 属性组详情。
func (h *ProductHandle) GetAttribute(c *gin.Context) {
	req, ok := binding.Query[productdto.GetAttributeReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListAttributes 属性组列表。
func (h *ProductHandle) ListAttributes(c *gin.Context) {
	req, ok := binding.Query[productdto.ListAttributeReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListAttributes(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteAttribute 删除属性组（被商品引用时拒绝）。
func (h *ProductHandle) DeleteAttribute(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteAttributeReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteAttribute(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

// BulkDeleteAttributes 批量删除属性组（页面 /admin/product-attributes/bulk-delete 的承接方）。
//
// 入口与三套归类共用（bulkDeleteByIDs）：归一、上限、回执键名必须逐字同口径，
// 否则超限那批会被静默截断、或摘要句恒为「已删除 0 项」。
func (h *ProductHandle) BulkDeleteAttributes(c *gin.Context) {
	h.bulkDeleteByIDs(c, h.svc.BulkDeleteProductAttributes)
}
