package producthttp

import (
	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type PublishAttemptHandle struct {
	reader productcontract.ProductPageReader
}

func NewPublishAttemptHandle(reader productcontract.ProductPageReader) *PublishAttemptHandle {
	return &PublishAttemptHandle{reader: reader}
}

func (h *PublishAttemptHandle) ProductPublishAttempts(c *gin.Context) {
	req, ok := binding.Query[productdto.PublishAttemptListReq](c)
	if !ok {
		return
	}
	res, err := h.reader.ProductPublishAttempts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "product", err)
		return
	}
	response.Success(c, res)
}
