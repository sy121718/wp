package producthttp

import (
	"errors"

	"net/http"

	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
)

type productTranslationPort = productservice.ProductTranslationStorePort

type ProductTranslationInstancePort = productservice.ProductTranslationInstancePort

type ProductTranslationHandle struct {
	products productcontract.ProductService
}

type productTranslationWiring interface {
	SetTranslationInstances(port productservice.ProductTranslationInstancePort)
	SetI18nStaleMarker(m productservice.I18nStaleMarker)
}

func NewProductTranslationHandle(products productcontract.ProductService,
	pages pagecontract.PageService, instances ProductTranslationInstancePort) *ProductTranslationHandle {
	if w, ok := products.(productTranslationWiring); ok {
		w.SetTranslationInstances(instances)
		if pages != nil {
			w.SetI18nStaleMarker(pages)
		}
	}
	return &ProductTranslationHandle{products: products}
}

func (h *ProductTranslationHandle) SetContentTranslationStore(port productTranslationPort) {
	if s, ok := h.products.(interface {
		SetTranslationStore(productservice.ProductTranslationStorePort)
	}); ok {
		s.SetTranslationStore(port)
	}
}

func (h *ProductTranslationHandle) translationsReader() (productcontract.ProductTranslationsReader, error) {
	reader, ok := h.products.(productcontract.ProductTranslationsReader)
	if !ok {
		return nil, errors.New("商品译文工作台读模型未注入（装配缺陷）")
	}
	return reader, nil
}

func (h *ProductTranslationHandle) ProductTranslations(c *gin.Context) {
	req, ok := binding.Query[productdto.ProductTranslationsPageReq](c)
	if !ok {
		return
	}
	view, err := h.page(c, req)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	scope, err := h.products.ResolveCatalogScope(c.Request.Context(), req.ProjectID)
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	pageview.RenderTemplate(c, ProductTranslationsPageTemplate, pageview.Objects(pageview.Object("Scope", scope), pageview.Object("Translations", view)))
}

func (h *ProductTranslationHandle) SaveProductTranslations(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.SaveProductTranslationsReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	view, err := h.save(c, &req)
	if err != nil || !view.Saved {
		replySaveRejected(c, view, err)
		return
	}
	pageview.Success(c)
}

func replySaveRejected(c *gin.Context, view *productcontract.TranslationSaveResult, err error) {
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	// 行级错误是业务事实：结构化交给 Jet 片段取词，不走 JSON 出口。
	c.Header("Cache-Control", "no-store")
	// 片段也走路由上已装的渲染器：取词与请求事实由它统一注入。
	pageview.RenderTemplate(c, "admin/product/translation_save_errors.html", pageview.Objects(
		pageview.Object("Title", productservice.TranslationsErrorsTitle()),
		pageview.Object("Rows", view.Errors),
	))
}

func (h *ProductTranslationHandle) page(c *gin.Context, req *productdto.ProductTranslationsPageReq) (*productcontract.ProductTranslations, error) {
	reader, err := h.translationsReader()
	if err != nil {
		return nil, err
	}
	return reader.QueryProductTranslations(c.Request.Context(), req)
}

func (h *ProductTranslationHandle) save(c *gin.Context, req *productdto.SaveProductTranslationsReq) (*productcontract.TranslationSaveResult, error) {
	reader, err := h.translationsReader()
	if err != nil {
		return nil, err
	}
	return reader.SaveProductTranslations(c.Request.Context(), req)
}
