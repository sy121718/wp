package pubhttp

import (
	"context"
	"net/http"
	"strings"

	"go_wp/internal/adminui"
	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// pubAuditFailedMsg 体检失败的**文案事实**（key + 中文兜底）。
//
// 为什么是事实而不是成品串：语言是请求级的，Go 侧取词意味着每处都要再想一遍
// 「这句属于哪个语言」，而漏一处、或某个 helper 忘了带 c，症状是「中文环境全对、
// 英文界面上整句变中文」——不报错、不记日志、测试也不红（批次约定见
// .cache/wp-refactor-A-brief.md）。取词在渲染期发生（片段模板用注入的 t）。
var pubAuditFailedMsg = adminui.Msg{Key: pubenums.ErrAuditFailed, Fallback: "SEO 体检失败，请查看服务端日志"}

// seoAuditHandler keeps the existing JSON contract while serving HTML only to htmx.
func seoAuditHandler(run func(context.Context, string) ([]pubservice.AuditIssue, int, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, ok := binding.Form[pubdto.SeoAuditReq](c)
		if !ok {
			return
		}
		projectID := strings.TrimSpace(req.Project)
		if projectID == "" {
			// hx-post 带表单、页面直链带查询串：两种来源都接受，字段口径由 DTO 定。
			projectID = strings.TrimSpace(c.Query("project"))
		}
		issues, scanned, err := run(c.Request.Context(), projectID)
		if err != nil {
			logger.Scene("publication").Error(err, "SEO 体检失败")
			if c.GetHeader("HX-Request") == "true" {
				// 片段槽位：模板直接渲染文本（见 admin/analytics/seo_audit_result.html），
				// 因此这里把**事实**取成当前语言文本交给它（这个槽位还没取词化，
				// 登记在批次报告里）。取词本身走 adminui.MsgText，不再经 shell。
				c.HTML(http.StatusOK, "admin/analytics/seo_audit_result.html", adminui.Prepare(c, gin.H{
					"AuditError":     true,
					"AuditErrorText": adminui.MsgText(c, pubAuditFailedMsg),
				}))
				return
			}
			response.ErrorWithMessage(c, http.StatusBadRequest, pubenums.ErrAuditFailed)
			return
		}
		if c.GetHeader("HX-Request") == "true" {
			// t 由 adminui.Prepare 注入（片段与整页共用同一条注入链）：Go 侧不再自带取词函数，
			// 模板里的 {{ .["t"](key, fallback) }} 按当前请求语言取值。
			c.HTML(http.StatusOK, "admin/analytics/seo_audit_result.html", adminui.Prepare(c, gin.H{
				"AuditError": false,
				"Issues":     issues, "Scanned": scanned, "Count": len(issues),
			}))
			return
		}
		response.Success(c, gin.H{"scanned": scanned, "issues": issues, "count": len(issues)})
	}
}
