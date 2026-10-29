package contenthttp

import (
	"github.com/gin-gonic/gin"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type ContentPublicHandle struct {
	svc contentcontract.PublicSearchReader
}

func NewContentPublicHandle(svc contentcontract.PublicSearchReader) *ContentPublicHandle {
	return &ContentPublicHandle{svc: svc}
}
func (h *ContentPublicHandle) Search(c *gin.Context) {
	req, ok := binding.Query[contentdto.PublicSearchReq](c)
	if !ok {
		return
	}
	data, err := h.svc.PublicSearch(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "content", err)
		return
	}
	response.Success(c, data)
}
