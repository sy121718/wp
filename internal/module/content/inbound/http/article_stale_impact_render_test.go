package contenthttp

import (
	contentdto "go_wp/internal/module/content/dto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestArticlesListRenderWithStaleImpactPages(t *testing.T) {
	list := []*contentdto.ContentResp{
		{ID: "a1", Slug: "hello-world", Revision: 1, UpdatedAt: "2026-09-13 10:00",
			Data: map[string]any{"title": "第一篇"}},
	}
	data := articleListPageData(list, map[string]string{"a1": "/blog/hello-world"}, articleMsg{})
	data.StaleImpact = &StaleImpactResp{
		Available: true,
		Pages:     []StalePageRowResp{{ID: "pg1", Path: "/zh/stale-only-page", ProjectID: "prj", ProjectName: "站点"}},
		Total:     1, Truncated: false, Limit: 8, Hint: articleMsg{},
	}
	body := renderAdminTemplate(t, "admin/content/articles.html", articleLayoutData(data))
	for _, want := range []string{
		"待重建影响面",
		`data-drawer-open hx-target="#admin-drawer-body" hx-swap="innerHTML" hx-get="/admin/articles/stale/drawer"`,
		"第一篇",
		"/admin/articles/new",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("待重建影响面存在时，列表页应完整渲染并包含 %q", want)
		}
	}

	if strings.Contains(body, "/zh/stale-only-page") {
		t.Error("列表页渲染了待重建清单条目 —— 清单应只在抽屉片段里渲染")
	}
}

func TestArticleStaleDrawerRendersRows(t *testing.T) {
	data := gin.H{
		"Rows": []gin.H{

			{"ID": "pg1", "Path": "/zh/stale-only-page", "ProjectName": "站点", "Published": true, "RebuildFailure": gin.H{}},
			{"ID": "pg2", "Path": "/never-live", "ProjectName": "站点", "Published": false, "RebuildFailure": gin.H{}},
		},
		"Total": 2, "Truncated": false, "Limit": 8, "MultiProject": false,
	}
	body := renderAdminTemplate(t, "admin/partials/stale_pages_drawer.html", data)

	for _, want := range []string{
		"/zh/stale-only-page", "/never-live",
		`href="/workbench?id=pg1"`,
		`data-drawer-readonly`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("抽屉片段缺少 %q", want)
		}
	}

	if strings.Contains(body, "<td>0</td>") || strings.Contains(body, "<td>1</td>") {
		t.Error("路径列渲染成了下标 —— range 又写成了单变量形式（Jet 会绑定下标）")
	}
}
