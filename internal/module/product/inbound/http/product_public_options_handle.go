package producthttp

import (
	"github.com/gin-gonic/gin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type ProductOptionsHandle struct {
	svc productcontract.PublicProductOptionsReader
}

func (h *ProductOptionsHandle) Get(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.PublicProductOptionsReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	data, err := h.svc.PublicProductOptions(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.Success(c, data)
}
