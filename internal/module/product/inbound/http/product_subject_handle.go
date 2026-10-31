package producthttp

import (
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

func (h *ProductHandle) Create(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

func (h *ProductHandle) Update(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
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

func (h *ProductHandle) Delete(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

func (h *ProductHandle) BulkDelete(c *gin.Context) {
	req, ok := binding.JSON[productdto.BulkDeleteReq](c)
	if !ok {
		return
	}

	res, err := h.svc.BulkDeleteProducts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, res)
}

func (h *ProductHandle) CreateVariant(c *gin.Context) {
	req, ok := binding.JSON[productdto.CreateVariantReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.CreateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

func (h *ProductHandle) UpdateVariant(c *gin.Context) {
	req, ok := binding.JSON[productdto.UpdateVariantReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.UpdateVariant(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

func (h *ProductHandle) DeleteVariant(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteVariantReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	if err := h.svc.DeleteVariant(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}

func (h *ProductHandle) GenerateVariants(c *gin.Context) {
	req, ok := binding.JSON[productdto.GenerateVariantsReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.GenerateVariants(c.Request.Context(), req)
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
	form, ok := binding.JSON[productdto.VariantPreviewFormReq](c)
	if !ok {
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
	req, ok := binding.JSON[productdto.SaveVariantListReq](c)
	if !ok {
		return
	}

	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.SaveVariantList(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg, res)
}

func (h *ProductHandle) AddRating(c *gin.Context) {
	req, ok := binding.JSON[productdto.AddRatingReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.AddRating(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg, res)
}

func (h *ProductHandle) DeleteRating(c *gin.Context) {
	req, ok := binding.JSON[productdto.DeleteRatingReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteRating(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessMsg(c, productActionDoneMsg)
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
	req, ok := binding.JSON[productdto.PricingPreviewReq](c)
	if !ok {
		return
	}
	res, err := h.svc.PreviewPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingPreviewSuccess, res)
}

func (h *ProductHandle) ApplyPricing(c *gin.Context) {
	req, ok := binding.JSON[productdto.PricingApplyReq](c)
	if !ok {
		return
	}
	req.OperatorID = builtin.OperatorName(c)
	res, err := h.svc.ApplyPricing(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
}

func (h *ProductHandle) BulkPricing(c *gin.Context) {
	req, ok := binding.JSON[productdto.BulkPricingJSONReq](c)
	if !ok {
		return
	}

	res, err := h.svc.BulkApplyPricing(c.Request.Context(), req.ServiceReq(builtin.OperatorName(c)))
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgPricingApplySuccess, res)
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
