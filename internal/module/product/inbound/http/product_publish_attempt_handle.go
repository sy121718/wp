package producthttp

import (
	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

type PublishAttemptHandle struct {
	reader productcontract.ProductPageReader
}

func NewPublishAttemptHandle(reader productcontract.ProductPageReader) *PublishAttemptHandle {
	return &PublishAttemptHandle{reader: reader}
}

func (h *PublishAttemptHandle) ProductPublishAttempts(c *gin.Context) {
	req, err := binding.ParseQuery[productdto.PublishAttemptListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.reader.ProductPublishAttempts(c.Request.Context(), req)
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	pageview.RenderTemplate(c, "admin/product/publish_attempt_rows.html", pageview.Objects(pageview.Object("Attempts", res), pageview.Object("ProjectID", req.ProjectID), pageview.Object("ProductID", req.ProductID), pageview.Object("NextPage", req.Page+1)))
}
