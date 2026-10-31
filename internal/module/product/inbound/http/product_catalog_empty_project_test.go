package producthttp

import (
	"net/http"
	"strings"
	"testing"

	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
)

func TestProductPagesWithoutProjectsSkipDataQueries(t *testing.T) {
	for _, tc := range []struct{ path, content string }{
		{"/admin/products", `class="empty-state"`},
		{"/admin/products/new", "基础信息"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			projects := productPageProjectsStub{rows: []projectdto.ProjectResp{}}
			// 使用真实 service 且不给 model：页面若仍进入无工程的数据查询就会失败。
			svc := productservice.NewService(nil, projects)
			h := NewProductPageHandle(svc, svc)
			rec := productPageRequest(t, h, tc.path)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("无工程页面应返回 HTML 200：%d %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.content) || !strings.Contains(rec.Body.String(), "</html>") {
				t.Fatalf("无工程页面未完整渲染：%s", rec.Body.String())
			}
		})
	}
}
