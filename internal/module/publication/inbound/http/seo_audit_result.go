package pubhttp

import (
	"context"
	"net/http"
	"strings"

	pubenums "go_wp/internal/module/publication/enums"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// seoAuditHandler keeps the existing JSON contract while serving HTML only to htmx.
func seoAuditHandler(run func(context.Context, string) ([]pubservice.AuditIssue, int, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := strings.TrimSpace(c.PostForm("project"))
		if projectID == "" {
			projectID = strings.TrimSpace(c.Query("project"))
		}
		issues, scanned, err := run(c.Request.Context(), projectID)
		if err != nil {
			logger.Scene("publication").Error(err, "SEO 体检失败")
			if c.GetHeader("HX-Request") == "true" {
				translate := shell.TranslateFor(c)
				c.HTML(http.StatusOK, "admin/analytics/seo_audit_result.html", gin.H{
					"t": translate, "AuditError": true,
					"AuditErrorText": translate(pubenums.ErrAuditFailed, "SEO 体检失败，请查看服务端日志"),
				})
				return
			}
			response.ErrorWithMessage(c, http.StatusBadRequest, pubenums.ErrAuditFailed)
			return
		}
		if c.GetHeader("HX-Request") == "true" {
			c.HTML(http.StatusOK, "admin/analytics/seo_audit_result.html", gin.H{
				"t": shell.TranslateFor(c), "AuditError": false,
				"Issues": issues, "Scanned": scanned, "Count": len(issues),
			})
			return
		}
		response.Success(c, gin.H{"scanned": scanned, "issues": issues, "count": len(issues)})
	}
}
