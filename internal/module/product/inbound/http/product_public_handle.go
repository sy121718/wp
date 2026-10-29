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
	req, ok := binding.Query[productdto.PublicVariantsReq](c)
	if !ok {
		return
	}
	data, err := h.svc.PublicVariants(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.Success(c, data)
}
