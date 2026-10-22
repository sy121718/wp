package producthttp

import (
	"context"
	"errors"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_attributes_handle.go — 商品属性库（属性组 / 属性值）：列表页与它的抽屉表单。
//
// 属性分两组形态：**规格属性**（isVariation，参与变体生成）与普通属性（只作展示与筛选）。
// 属性值行（ProductAttributesValueRows）是 HTMX 片段，商品编辑页与新建页都复用它。

// 接口面（原先的 product_handle.go 属性段）在前，后台页面在后。

// ProductAttributesPage 属性管理页：工程切换 + 筛选栏 + 属性组列表 + 值编辑器 + 内联新建表单。

// ProductAttributesPage 属性管理页：工程切换 + 筛选栏 + 属性组列表 + 值编辑器 + 内联新建表单。
func (h *ProductPageHandle) ProductAttributesPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductAttributesPageReq](c)
	if !ok {
		return
	}
	// 取数编排、成品行与两个抽屉的初值都在 service.ProductAttributesPage。
	view, err := h.reader.ProductAttributesPage(c.Request.Context(), req)
	if err != nil {
		// 失败出口是 JSON：不装配、不渲染同一页（档位由声明处给，见 productenums.StatusFor）。
		pageview.Fail(c, err)
		return
	}
	if view == nil {
		// 契约上视图恒非 nil；真为空是编程错误，交出口按 500 处理而不是在这里解引用。
		pageview.Fail(c, errors.New("属性页读模型返回空视图"))
		return
	}
	// 行与两个抽屉的初值都在读模型里装（AttrPageRows / AttrCreateDrawerForm）。
	pageview.Render(c, view)
}

// ProductAttributesValueRows 值编辑器片段：add / remove 一次，回渲染整段行列表。
//
// 行编辑只重建原始表单值与位置；落库时的归一 / 去重 / 排序仍由 service 负责。
func (h *ProductPageHandle) ProductAttributesValueRows(c *gin.Context) {
	req, ok := binding.Form[productdto.AttributeRowsFormReq](c)
	if !ok {
		return
	}
	pageview.Render(c, productservice.AttrValueRows(req))
}

// attributeOptions 属性组勾选列表（新建抽屉 / 编辑页；本批起不再让人手打 UUID）。
//
// 取数与标签口径（名字 + 标识 key，只给名字会在同名属性组之间选错）唯一一份在
// service 的 AttributeOptionViews —— 编辑页这条老路径与新建页读模型共用同一个结论，
// 两处各拼一遍时「带上 key」这条规则改一处就漏一处。
//
// 上限同样取 service 的常量：抽屉与新建页是同一类「一屏全量勾选」，两处各写一个 200
// 时改一处会静默分叉。
func (h *ProductPageHandle) attributeOptions(ctx context.Context, projectID string) (out []productdto.AttributeOptionResp, err error) {
	out = []productdto.AttributeOptionResp{}
	if strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	list, lerr := h.products.ListAttributes(ctx, &productdto.ListAttributeReq{
		ProjectID: projectID, Size: productservice.PageAttributeOptionSize,
	})
	if lerr != nil {
		return nil, lerr
	}
	return productservice.AttributeOptionViews(list), nil
}

// ==========================================================================
// 属性接口：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：值组的归一 / 去重 / 排序、引用校验一律在 service。
// ==========================================================================
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
