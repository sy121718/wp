package producthttp

import (
	"errors"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	"net/http"

	"github.com/gin-gonic/gin"
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
	req, ok := binding.JSON[productdto.SaveProductTranslationsReq](c)
	if !ok {
		return
	}
	view, err := h.save(c, req)
	if err != nil || !view.Saved {
		replySaveRejected(c, view, err)
		return
	}
	response.SuccessMsg(c, productservice.TranslationsSavedMsg(view.Written, view.StaleMarkFailed))
}

func replySaveRejected(c *gin.Context, view *productcontract.TranslationSaveResult, err error) {
	if err != nil {
		pageview.Fail(c, err)
		return
	}
	response.ErrorRows(c, http.StatusBadRequest, productservice.TranslationsErrorsTitle(), view.Errors)
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
