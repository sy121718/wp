package producthttp

import (
	"context"
	"go_wp/pkg/binding"
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const productErrScene = "product"

// Create 绑定新建表单，交 Service 创建并由 Jet 提供真实编辑链接。
func (h *ProductHandle) Create(c *gin.Context) {
	runProductWriteResult(c,
		func(c *gin.Context, req *productdto.CreateReq) { req.OperatorID = builtin.OperatorName(c) },
		func(ctx context.Context, req *productdto.CreateReq) (any, error) { return h.svc.Create(ctx, req) },
		func(c *gin.Context, res any) { c.HTML(http.StatusOK, "admin/product/product_created.html", res) })
}

// Update 接收编辑器的结构化对象，结果交 Jet，失败不替换已编辑内容。
// 编辑器提交的是 JSON 对象，失败必须落 HTML 错误区而不是 JSON 参数出口，
// 因此用 ParseJSON 取错误事实，而不是 JSON[T] 的 JSON 参数出口。
func (h *ProductHandle) Update(c *gin.Context) {
	req, ok := updateForm(c)
	if !ok {
		return
	}
	if _, err := h.svc.Update(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

// updateForm 绑定编辑器提交的结构化对象并注入可信操作人；失败已由这里写出 400。
func updateForm(c *gin.Context) (*productdto.UpdateReq, bool) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseJSON[productdto.UpdateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return nil, false
	}
	req.OperatorID = builtin.OperatorName(c)
	return req, true
}

func (h *ProductHandle) Get(c *gin.Context) {
	req, ok := binding.Query[productdto.GetReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

func (h *ProductHandle) List(c *gin.Context) {
	req, ok := binding.Query[productdto.ListReq](c)
	if !ok {
		return
	}
	list, err := h.svc.QueryProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// Delete 只绑定写入 body，操作人始终取自当前登录态。
func (h *ProductHandle) Delete(c *gin.Context) {
	runProductWriteErrWith(c,
		func(c *gin.Context, req *productdto.DeleteReq) { req.OperatorID = builtin.OperatorName(c) },
		func(ctx context.Context, req *productdto.DeleteReq) error { return h.svc.Delete(ctx, req) },
		productWriteOK)
}

// BulkDelete 保留 Service 的整批原子语义，失败事实交共享 Jet 出口。
func (h *ProductHandle) BulkDelete(c *gin.Context) {
	runProductWriteEnd(c, func(ctx context.Context, req *productdto.BulkDeleteReq) error {
		_, err := h.svc.BulkDeleteProducts(ctx, req)
		return err
	}, productWriteOK)
}

func (h *ProductHandle) CreateVariant(c *gin.Context) {
	runProductWriteErrWith(c,
		func(c *gin.Context, req *productdto.CreateVariantReq) { req.OperatorID = builtin.OperatorName(c) },
		func(ctx context.Context, req *productdto.CreateVariantReq) error {
			_, err := h.svc.CreateVariant(ctx, req)
			return err
		},
		productWriteOK)
}

func (h *ProductHandle) UpdateVariant(c *gin.Context) {
	var req productdto.UpdateVariantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.UpdateVariant(c.Request.Context(), &req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

func (h *ProductHandle) DeleteVariant(c *gin.Context) {
	runProductWriteErrWith(c,
		func(c *gin.Context, req *productdto.DeleteVariantReq) { req.OperatorID = builtin.OperatorName(c) },
		func(ctx context.Context, req *productdto.DeleteVariantReq) error {
			return h.svc.DeleteVariant(ctx, req)
		},
		productWriteOK)
}

func (h *ProductHandle) GenerateVariants(c *gin.Context) {
	var req productdto.GenerateVariantsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.GenerateVariants(c.Request.Context(), &req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgVariantGenerateSuccess, res)
}

func (h *ProductPageHandle) ProductsPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductListPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	result, err := h.products.QueryProducts(c.Request.Context(), &productdto.ListReq{ProjectID: scope.ProjectID, Keyword: req.Keyword, Status: req.Status, Page: req.Page, Size: 20})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	categories, err := h.products.CategoryNodes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	brands, err := h.products.CatalogBrands(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	tags, err := h.products.CatalogTags(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	warehouses, err := h.products.CatalogWarehouses(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, ProductListPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Query", req),
		pageview.Object("Result", result), pageview.Object("Categories", categories), pageview.Object("Brands", brands),
		pageview.Object("Tags", tags), pageview.Object("WarehouseOptions", warehouses),
		pageview.Object("Rules", h.products.ListPricingRuleTypes(c.Request.Context())),
		pageview.Object("Roundings", h.products.ListPricingRoundingOptions(c.Request.Context()))))
}

func (h *ProductPageHandle) ProductNewPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductNewPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	warehouses, err := h.products.CatalogWarehouses(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	attributes, err := h.products.CatalogAttributes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	skus, err := h.products.CatalogWarehouseSKUs(c.Request.Context(), scope.ProjectID, warehouses)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	templates, err := h.products.CatalogEntityTemplates(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, ProductNewPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("WarehouseOptions", warehouses),
		pageview.Object("Attributes", attributes), pageview.Object("WarehouseSKUOptions", skus), pageview.Object("TemplateCatalog", templates)))
}

func (h *ProductPageHandle) ProductEditPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductEditPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ProjectID: scope.ProjectID, ID: req.ProductID})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	categories, err := h.products.CategoryNodes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	brands, err := h.products.CatalogBrands(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	tags, err := h.products.CatalogTags(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	attributes, err := h.products.CatalogAttributes(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	warehouses, err := h.products.CatalogWarehouses(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	skus, err := h.products.CatalogWarehouseSKUs(c.Request.Context(), scope.ProjectID, warehouses)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	panel, err := h.products.QueryDetailTemplatePanel(c.Request.Context(), scope.ProjectID, product.ID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, ProductEditPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Product", product), pageview.Object("Categories", categories),
		pageview.Object("Brands", brands), pageview.Object("Tags", tags), pageview.Object("Attributes", attributes),
		pageview.Object("WarehouseOptions", warehouses), pageview.Object("WarehouseSKUOptions", skus), pageview.Object("TplPanel", panel)))
}

func (h *ProductPageHandle) ProductDetailPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductDetailPageReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ProjectID: scope.ProjectID, ID: req.ProductID})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	panel, err := h.products.QueryDetailTemplatePanel(c.Request.Context(), scope.ProjectID, product.ID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, ProductDetailPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Product", product), pageview.Object("TplPanel", panel)))
}

func (h *ProductPageHandle) PreviewVariant(c *gin.Context) {
	// 变体预览由 product_variant_write.js 以 FormData 提交；显式表单入口保留
	// DTO 的 BindForm 钩子（attr:<id> 重复键 → Selections）。
	form, err := binding.ParseForm[productdto.VariantPreviewFormReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.products.PreviewVariantCombinations(c.Request.Context(), form.ServiceReq())
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *ProductHandle) SaveVariantList(c *gin.Context) {
	var req productdto.SaveVariantListReq
	if err := c.ShouldBindJSON(&req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.SaveVariantList(c.Request.Context(), &req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg, res)
}

// AddRating 接收标准表单并返回完整评分区域，不替换商品编辑器。
func (h *ProductHandle) AddRating(c *gin.Context) {
	runProductWriteResult(c,
		func(c *gin.Context, req *productdto.AddRatingReq) { req.OperatorID = builtin.OperatorName(c) },
		func(ctx context.Context, req *productdto.AddRatingReq) (any, error) { return h.svc.AddRating(ctx, req) },
		func(c *gin.Context, res any) {
			pageview.RenderTemplate(c, "admin/product/product_rating_panel.html", pageview.Objects(pageview.Object("Rating", res)))
		})
}

// DeleteRating 将删除后的评分事实交同一 Jet 区域，失败保留当前输入。
func (h *ProductHandle) DeleteRating(c *gin.Context) {
	runProductWriteResult(c, nil,
		func(ctx context.Context, req *productdto.DeleteRatingReq) (any, error) {
			return h.svc.DeleteRating(ctx, req)
		},
		func(c *gin.Context, res any) {
			pageview.RenderTemplate(c, "admin/product/product_rating_panel.html", pageview.Objects(pageview.Object("Rating", res)))
		})
}

func (h *ProductPageHandle) ProductPreviewFrame(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, ok := binding.Query[productdto.ProductPreviewFrameReq](c)
	if !ok {
		return
	}
	res, err := productservice.PreviewSavedProduct(c.Request.Context(), h.products, h.reader, h.instances, req.ProjectID, req.ProductID)
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

func (h *ProductPageHandle) ProductPricingPage(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductProjectReq](c)
	if !ok {
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	var history []*productdto.PriceAdjustmentResp
	if scope.ProjectID != "" {
		history, err = h.products.ListPriceAdjustments(c.Request.Context(), &productdto.ListPriceAdjustmentReq{ProjectID: scope.ProjectID, Limit: 10, IncludeItems: true})
		if err != nil {
			pageview.Fail(c, err)
			return
		}
	}
	pageview.RenderTemplate(c, ProductPricingPageTemplate, pageview.Objects(
		pageview.Object("Scope", scope),
		pageview.Object("Rules", h.products.ListPricingRuleTypes(c.Request.Context())),
		pageview.Object("Roundings", h.products.ListPricingRoundingOptions(c.Request.Context())),
		pageview.Object("History", history),
	))
}

func (h *ProductHandle) ListPricingRuleTypes(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRuleTypes(c.Request.Context()))
}

func (h *ProductHandle) ListPricingRoundingOptions(c *gin.Context) {
	response.SuccessWithMessage(c, productenums.MsgListSuccess, h.svc.ListPricingRoundingOptions(c.Request.Context()))
}

func (h *ProductHandle) PreviewPricing(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[productdto.PricingPreviewReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	res, err := h.svc.PreviewPricing(c.Request.Context(), req)
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.RenderTemplate(c, "admin/product/pricing_preview_result.html", pageview.Objects(pageview.Object("Preview", res)))
}

func (h *ProductHandle) ApplyPricing(c *gin.Context) {
	// 与预览共用同一份表单契约：规则参数由 PricingRuleReq.BindForm 从字段派生。
	req, ok := applyPricingForm(c)
	if !ok {
		return
	}
	if _, err := h.svc.ApplyPricing(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

// applyPricingForm 绑定定价应用表单并注入可信操作人；失败已由这里写出 400。
func applyPricingForm(c *gin.Context) (*productdto.PricingApplyReq, bool) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[productdto.PricingApplyReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return nil, false
	}
	req.OperatorID = builtin.OperatorName(c)
	return req, true
}

func (h *ProductHandle) BulkPricing(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := bindJSONFact[productdto.BulkPricingJSONReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := h.svc.BulkApplyPricing(c.Request.Context(), req.ServiceReq(builtin.OperatorName(c))); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) ListPriceAdjustments(c *gin.Context) {
	req, ok := binding.Query[productdto.ListPriceAdjustmentReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListPriceAdjustments(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

func (h *ProductHandle) GetPriceAdjustment(c *gin.Context) {
	req, ok := binding.Query[productdto.GetPriceAdjustmentReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetPriceAdjustment(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}
