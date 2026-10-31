package pubhttp

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

type SeoAuditHandle struct {
	run func(context.Context, string) (pubservice.AuditResult, error)
}

func NewSeoAuditHandle(run func(context.Context, string) (pubservice.AuditResult, error)) *SeoAuditHandle {
	return &SeoAuditHandle{run: run}
}

func (h *SeoAuditHandle) SeoAudit(c *gin.Context) {
	req, ok := binding.JSON[pubdto.SeoAuditReq](c)
	if !ok {
		return
	}
	query, ok := binding.Query[pubdto.SeoAuditReq](c)
	if !ok {
		return
	}
	result, err := h.run(c.Request.Context(), req.ResolveProject(query))
	if err != nil {

		response.ErrorStatusAuto(c, "publication", err)
		return
	}
	response.Success(c, result)
}
