package analyticshttp

// seo_page_handle.go — 最小 SEO 控制台（SEO-020）。自 dashboard 迁回 analyticshttp：
// SEO 控制台读 analytics + project 契约，与统计页同域相邻；SEO 体检动作由
// publication 现有端点执行，本页只编排已有只读契约。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

const seoPageTitle = "SEO 控制台"

// seoPageHandle 只编排已有只读契约；SEO 体检由 publication 现有端点执行。
type seoPageHandle struct {
	analytics analyticscontract.AnalyticsService
	projects  projectcontract.ProjectService
}

// NewSEOPageHandle 创建 SEO 控制台处理器。
func NewSEOPageHandle(analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) *seoPageHandle {
	return &seoPageHandle{analytics: analytics, projects: projects}
}

// SEOPage 渲染当前站点的 SEO 体检入口与已有访问统计。
func (h *seoPageHandle) SEOPage(c *gin.Context) {
	data := gin.H{
		"title":           seoPageTitle,
		"menu":            "seo",
		"Projects":        []projectcontract.ProjectResp{},
		"SelectedProject": "",
		"Paths":           []analyticscontract.PathCount{},
		"HasPaths":        false,
		"AnalyticsError":  false,
	}

	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		logger.Error(err, "SEO 控制台读取站点列表失败")
		data["ProjectError"] = true
		c.HTML(http.StatusOK, "admin/analytics/seo.html", shell.Prepare(c, data))
		return
	}
	data["Projects"] = projects

	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	data["SelectedProject"] = selected

	if selected != "" && h.analytics != nil {
		summary, summaryErr := h.analytics.Summary(c.Request.Context(), &analyticscontract.SummaryReq{
			ProjectID: selected,
			PathPage:  1,
			PathLimit: 10,
		})
		if summaryErr != nil {
			logger.Error(summaryErr, "SEO 控制台读取热门路径失败，站点："+selected)
			data["AnalyticsError"] = true
		} else if summary != nil {
			data["Paths"] = summary.Paths
			data["HasPaths"] = len(summary.Paths) > 0
		}
	}

	c.HTML(http.StatusOK, "admin/analytics/seo.html", shell.Prepare(c, data))
}
