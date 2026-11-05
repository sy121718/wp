package producthttp

import (
	"context"
	"errors"
	productcontract "go_wp/internal/module/product/contract"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

func publishRecordRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products/publish-records",
		pageview.UseRenderer(adminui.RenderPage), h.ProductPublishRecordPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestProductPublishRecordPageRendersStates(t *testing.T) {
	published := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	view := publishRecordListStub(func(v *productcontract.PublishRecordList) {
		v.Total = 3
		v.Records = []productdto.PublishRecordRowResp{
			{
				ProductID: "prod-1", ProductName: "已上线商品", ProductSlug: "live",
				HasInstance: true, Online: true, Stale: true,
				PublishedAt: published, ArtifactVersion: 7, ArtifactHash: "abc123",
				Langs: []productdto.PublishRecordLangResp{{
					Lang: "zh-CN", Online: true, OnlinePath: "/products/live",
					PublishedAt: published, ArtifactVersion: 7, ArtifactHash: "abc123",
				}},
			},
			{
				ProductID: "prod-2", ProductName: "有实例没上线", ProductSlug: "draft",
				HasInstance: true,
				Langs:       []productdto.PublishRecordLangResp{{Lang: "zh-CN"}},
			},
			{ProductID: "prod-3", ProductName: "没建实例", ProductSlug: "none"},
		}
	})
	reader := &publicationReaderStub{publishView: view}
	h := &ProductPageHandle{reader: reader, products: reader}

	rec := publishRecordRequest(t, h, "/admin/products/publish-records?project=p1&status=active&keyword=%E5%95%86")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("成功档应为 200，实际 %d，body=%s", rec.Code, headOf(body))
	}
	for _, want := range []string{
		"已上线商品", "有实例没上线", "没建实例",
		"/products/live",
		"v7",
		"2026-10-20",
		"/api/product/detail-template/apply",
		"待重建",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q：%s", want, headOf(body))
		}
	}

	if n := strings.Count(body, "/api/product/detail-template/apply"); n != 2 {
		t.Fatalf("「重新发布」应只出现在两行有实例的记录上，实际 %d 处", n)
	}
	if len(reader.publishReqs) != 1 {
		t.Fatalf("发布记录应只查一次，实际 %d 次", len(reader.publishReqs))
	}
	got := reader.publishReqs[0]
	if got.ProjectID != "p1" || got.Status != "active" || got.Keyword != "商" {
		t.Fatalf("入参转发不对：%+v", *got)
	}
}

// TestProductPublishRecordPageRendersEmptyState 守住「没有发布事实时是空态，不是提示页」：
// 发布记录页已改成模块内一次只读语句，不再有「端口没接」这一档，页面照常 200 渲染。
func TestProductPublishRecordPageRendersEmptyState(t *testing.T) {
	view := publishRecordListStub(func(*productcontract.PublishRecordList) {})
	reader := &publicationReaderStub{publishView: view}
	h := &ProductPageHandle{reader: reader, products: reader}

	rec := publishRecordRequest(t, h, "/admin/products/publish-records?project=p1")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("空档也应是 200（空态而非错误），实际 %d，body=%s", rec.Code, headOf(body))
	}
	if !strings.Contains(body, "publish-records") && !strings.Contains(body, "发布") {
		t.Fatalf("空态页应渲染发布记录模板：%s", headOf(body))
	}
}

func TestProductPublishRecordPageLoadFailureIsJSON500(t *testing.T) {
	reader := &publicationReaderStub{publishErr: errors.New("驱动：读发布登记失败")}
	h := &ProductPageHandle{reader: reader, products: reader}

	rec := publishRecordRequest(t, h, "/admin/products/publish-records?project=p1")
	assertProductJSONFailure(t, "发布记录页主数据失败", rec, http.StatusInternalServerError, "内部错误")
}

type publicationReaderStub struct {
	productcontract.ProductService
	productcontract.ProductPageReader
	publishView *productcontract.PublishRecordList
	publishErr  error
	publishReqs []*productdto.ProductPublishRecordPageReq
}

func (s *publicationReaderStub) ResolveCatalogScope(context.Context, string) (*productcontract.CatalogScope, error) {
	return &productcontract.CatalogScope{ProjectID: "p1", Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}}, nil
}
func (s *publicationReaderStub) QueryPublishRecords(_ context.Context, req *productdto.ProductPublishRecordPageReq) (*productcontract.PublishRecordList, error) {
	s.publishReqs = append(s.publishReqs, req)
	return s.publishView, s.publishErr
}
func publishRecordListStub(change func(*productcontract.PublishRecordList)) *productcontract.PublishRecordList {
	r := &productcontract.PublishRecordList{Records: []productdto.PublishRecordRowResp{}, Page: 1, Limit: 20}
	change(r)
	return r
}
