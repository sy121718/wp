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

// SetSeoTitleSources 注入可选标题索引契约，缺少覆盖范围由 service 明示。
func (h *ProductPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

func (h *ProductPageHandle) seoTitleSources() productservice.SeoTitleExtraSources {
	return productservice.SeoTitleExtraSources{Pages: h.seoPages, Contents: h.seoContents}
}

// SeoScore 返回商品评分事实。
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

// CategorySeoScore 返回分类评分事实。
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

// BrandSeoScore 返回品牌评分事实。
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
