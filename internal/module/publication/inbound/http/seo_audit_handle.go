package pubhttp

import (
	"context"
	"net/http"

	pubdto "go_wp/internal/module/publication/dto"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

type SeoAuditHandle struct {
	run func(context.Context, string) (pubservice.AuditResult, error)
}

func NewSeoAuditHandle(run func(context.Context, string) (pubservice.AuditResult, error)) *SeoAuditHandle {
	return &SeoAuditHandle{run: run}
}

// bindSeoAuditReq 绑定 SEO 审计表单；失败已由这里写出 400。
func bindSeoAuditReq(c *gin.Context) (*pubdto.SeoAuditReq, bool) {
	req, err := binding.ParseForm[pubdto.SeoAuditReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "publication", err)
		return nil, false
	}
	return req, true
}

// SeoAudit 接收标准表单，结果由 Jet 片段渲染成结论区。
//
// 出口形态是**片段**（seo.html 用 hx-post + data-seo-audit-result 整块换入），
// 失败同样走片段而非 JSON。请求侧只接受标准表单编码：JSON 体在这里被显式拒绝，
// 不静默忽略字段。
func (h *SeoAuditHandle) SeoAudit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, ok := bindSeoAuditReq(c)
	if !ok {
		return
	}
	result, err := h.run(c.Request.Context(), req.ResolveProject(nil))
	if err != nil {
		pageview.FailScene(c, "publication", err)
		return
	}
	pageview.RenderFragment(c, "admin/analytics/seo_audit_result.html", pageview.Objects(
		pageview.Object("Result", result),
	))
}
