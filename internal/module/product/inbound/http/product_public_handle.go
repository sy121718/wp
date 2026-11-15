package producthttp

import (
	"github.com/gin-gonic/gin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type ProductPublicHandle struct {
	svc productcontract.PublicVariantReader
}

func NewProductPublicHandle(svc productcontract.PublicVariantReader) *ProductPublicHandle {
	return &ProductPublicHandle{svc: svc}
}

func (h *ProductPublicHandle) Variants(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.PublicVariantsReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	data, err := h.svc.PublicVariants(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.Success(c, data)
}

type ProductCatalogHandle struct {
	svc productcontract.PublicCatalogReader
}

func NewProductCatalogHandle(svc productcontract.PublicCatalogReader) *ProductCatalogHandle {
	return &ProductCatalogHandle{svc: svc}
}
func (h *ProductCatalogHandle) List(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.PublicCatalogReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	data, err := h.svc.PublicCatalog(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.Success(c, data)
}
