package producthttp

import (
	contentcontract "go_wp/internal/module/content/contract"
	pagecontract "go_wp/internal/module/page/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/seo/scoring"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *ProductPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

func (h *ProductPageHandle) seoTitleSources() productservice.SeoTitleExtraSources {
	return productservice.SeoTitleExtraSources{Pages: h.seoPages, Contents: h.seoContents}
}

func (h *ProductPageHandle) SeoScore(c *gin.Context) {
	req, ok := binding.JSON[productdto.ScorePanelReq](c)
	if !ok {
		return
	}
	res, err := productservice.SeoScore(c.Request.Context(), h.products, h.instances, h.seoTitleSources(), scoring.KindProduct, req, response.RequestLanguage(c))
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *ProductPageHandle) CategorySeoScore(c *gin.Context) {
	req, ok := binding.JSON[productdto.ScorePanelReq](c)
	if !ok {
		return
	}
	res, err := productservice.SeoScore(c.Request.Context(), h.products, h.instances, h.seoTitleSources(), scoring.KindCategory, req, response.RequestLanguage(c))
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *ProductPageHandle) BrandSeoScore(c *gin.Context) {
	req, ok := binding.JSON[productdto.ScorePanelReq](c)
	if !ok {
		return
	}
	res, err := productservice.SeoScore(c.Request.Context(), h.products, h.instances, h.seoTitleSources(), scoring.KindBrand, req, response.RequestLanguage(c))
	if err != nil {
		response.ErrorStatusAuto(c, productErrScene, err)
		return
	}
	response.Success(c, res)
}
