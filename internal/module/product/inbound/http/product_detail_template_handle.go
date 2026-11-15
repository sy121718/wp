package producthttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	ctoutbound "go_wp/internal/module/product/outbound/contenttemplate"
	dtoutbound "go_wp/internal/module/product/outbound/detailtemplate"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

var productActionDoneMsg = productenums.ActionDoneMessage()

var (
	_ productservice.ProductReadPort            = (productcontract.ProductService)(nil)
	_ productservice.DetailTemplateInstancePort = (ProductDetailTemplatePort)(nil)
)

func (h *ProductHandle) SetDetailTemplateWriteDeps(deps productservice.DetailTemplateDeps) {
	h.templateDeps = deps
}

func (h *ProductHandle) SetDetailTemplateModePort(port presentationcontract.DetailTemplateModePort) {
	h.modePort = port
}

func (h *ProductHandle) ReapplyDetailTemplatePreset(c *gin.Context) {
	req, err := binding.ParseForm[productdto.DetailTemplateModeReapplyReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	detailTemplateFinish(c, productservice.ReapplyDetailTemplatePreset(c.Request.Context(), h.modePort, req))
}

func (h *ProductHandle) RollbackDetailTemplateDocument(c *gin.Context) {
	req, err := binding.ParseForm[productdto.DetailTemplateModeRollbackReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	detailTemplateFinish(c, productservice.RollbackDetailTemplateDocument(c.Request.Context(), h.modePort, req))
}

func detailTemplateFinish(c *gin.Context, err error) {
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	pageview.Success(c)
}

func (h *ProductHandle) CreateDetailTemplate(c *gin.Context) {
	req, err := binding.ParseForm[productdto.DetailTemplateCreateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	detailTemplateFinish(c, productservice.CreateDetailTemplate(c.Request.Context(), h.templateDeps, req.ProjectID, req.Name, req.CopyFrom))
}

func (h *ProductHandle) PublishDetailTemplate(c *gin.Context) {
	req, err := binding.ParseForm[productdto.DetailTemplatePublishReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	detailTemplateFinish(c, productservice.PublishDetailTemplate(c.Request.Context(), h.templateDeps, req.ProjectID, req.ProductID, req.TemplateID, req.URLPath))
}

func (h *ProductHandle) ApplyDetailTemplate(c *gin.Context) {
	req, err := binding.ParseForm[productdto.DetailTemplateApplyReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	detailTemplateFinish(c, productservice.ApplyDetailTemplate(c.Request.Context(), h.templateDeps.Instances, req.ProjectID, req.ProductID, req.TemplateID))
}

func (h *ProductHandle) UpdateDetailTemplateURL(c *gin.Context) {
	req, err := binding.ParseForm[productdto.DetailTemplateUpdateURLReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	detailTemplateFinish(c, productservice.UpdateDetailTemplateURL(c.Request.Context(), h.templateDeps.Instances, req.ProjectID, req.ProductID, req.NewPath, req.WithRedirect))
}

const productDetailTemplatePath = "/admin/products/template"

type ProductPagePorts interface {
	ProductTranslationInstancePort
	ProductDetailTemplatePort
}

type ProductDetailTemplatePort interface {
	GetByEntity(ctx context.Context, req *presentationcontract.GetByEntityReq) (res *presentationcontract.InstanceResp, err error)
	PreviewInstance(ctx context.Context, req *presentationcontract.PreviewInstanceReq) (res *presentationcontract.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationcontract.CreateInstanceReq) (res *presentationcontract.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationcontract.RebuildReq) (res *presentationcontract.InstanceResp, err error)

	UpdateURL(ctx context.Context, req *presentationcontract.UpdateURLReq) (res *presentationcontract.InstanceResp, err error)
}

type ProductDetailTemplateModePort = presentationcontract.DetailTemplateModePort

func (h *ProductPageHandle) SetDetailTemplateModePort(port ProductDetailTemplateModePort) {
	h.modePort = port
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetDetailTemplatePort(dtoutbound.New(h.templates, h.instances, port))
	}
}

func (h *ProductPageHandle) SetDetailTemplateDeps(templates contenttemplatecontract.ContentTemplateService,
	instances ProductDetailTemplatePort) {
	h.templates = templates
	h.instances = instances

	if templates == nil {
		h.entityTemplates = nil
	} else {
		port := ctoutbound.New(templates)
		h.entityTemplates = port
		if wiring, ok := h.products.(productPagePortWiring); ok {
			wiring.SetEntityTemplatePort(port)
		}
	}

	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetDetailTemplatePort(dtoutbound.New(templates, instances, h.modePort))
	}
}

func (h *ProductPageHandle) ProductDetailTemplatePage(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.ProductDetailTemplatePageReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ID: req.ProductID, ProjectID: scope.ProjectID})
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	candidates, err := h.reader.QueryDetailTemplates(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	defaultID, err := h.reader.QueryDefaultDetailTemplate(c.Request.Context(), scope.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	bound, err := h.reader.QueryBoundDetailTemplate(c.Request.Context(), scope.ProjectID, product.ID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	path := h.reader.ProductDetailPath(c.Request.Context(), scope.ProjectID, product)
	pageview.RenderTemplate(c, ProductDetailTemplatePageTemplate, pageview.Objects(
		pageview.Object("Scope", scope), pageview.Object("Product", product),
		pageview.Object("Templates", candidates), pageview.Object("DefaultTemplateID", defaultID),
		pageview.Object("Bound", bound), pageview.Object("DetailPath", path),
		pageview.Object("Available", h.templates != nil && h.instances != nil),
	))
}

func (h *ProductPageHandle) ProductDetailTemplatePreview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[productdto.DetailTemplatePreviewReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, productenums.Err(productenums.ErrInvalidParam))
		return
	}
	res, err := productservice.PreviewProductDetailTemplate(c.Request.Context(), h.products, h.instances, req.ProjectID, req.ProductID, req.TemplateID)
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	// 路由没有声明 HTML 协议：这是数据接口，成功仍交 JSON 结果。
	response.Success(c, res)
}

const MsgProductsTitle = "MsgProductsTitle"
