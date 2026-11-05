package producthttp

import (
	"net/http"

	contentcontract "go_wp/internal/module/content/contract"
	pagecontract "go_wp/internal/module/page/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/seo/scoring"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
)

func (h *ProductPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

func (h *ProductPageHandle) seoTitleSources() productservice.SeoTitleExtraSources {
	return productservice.SeoTitleExtraSources{Pages: h.seoPages, Contents: h.seoContents}
}

// SeoScore 返回商品评分事实的 Jet HTML，不保存编辑内容。
func (h *ProductPageHandle) SeoScore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.ScorePanelReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	res, err := productservice.SeoScore(c.Request.Context(), h.products, h.instances, h.seoTitleSources(), scoring.KindProduct, &req, response.RequestLanguage(c))
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	c.HTML(http.StatusOK, "admin/product/product_seo_panel.html", res)
}

// CategorySeoScore 返回分类评分事实的 Jet HTML。
func (h *ProductPageHandle) CategorySeoScore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.ScorePanelReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	res, err := productservice.SeoScore(c.Request.Context(), h.products, h.instances, h.seoTitleSources(), scoring.KindCategory, &req, response.RequestLanguage(c))
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	c.HTML(http.StatusOK, "admin/product/product_seo_panel.html", res)
}

// BrandSeoScore 返回品牌评分事实的 Jet HTML。
func (h *ProductPageHandle) BrandSeoScore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req productdto.ScorePanelReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	res, err := productservice.SeoScore(c.Request.Context(), h.products, h.instances, h.seoTitleSources(), scoring.KindBrand, &req, response.RequestLanguage(c))
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	c.HTML(http.StatusOK, "admin/product/product_seo_panel.html", res)
}
