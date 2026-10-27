// product_publish_record_render_test.go — 发布记录页（B9）真的渲染出三种状态与「重新发布」入口。
//
// 钉两件事：
//  1. 视图形状与模板读的字段是同一份（Jet 在具名 map 上取不到键会渲染失败、在结构体上取不到
//     字段直接编译不过 —— 后者才是我们要的档位，所以整页必须真渲染一遍）；
//  2. 三种发布状态在 HTML 里可辨认（未建实例 / 已上线 / 有实例未上线 + 待重建），
//     且「重新发布」只对已有实例的行出现（没有实例时按模板再发一次是无处可发的）。
package producthttp

import (
	"errors"
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
		adminui.PageSlot(ProductPublishRecordPageTemplate), h.ProductPublishRecordPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestProductPublishRecordPageRendersStates(t *testing.T) {
	published := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	view := productPublishRecordPageViewStub(func(v *productdto.ProductPublishRecordPageResp) {
		v.Projects = []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}
		v.SelectedProject = "p1"
		v.SiteName = "工程一"
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
	reader := &productPageReaderStub{publishView: view}
	h := &ProductPageHandle{reader: reader, products: reader}

	rec := publishRecordRequest(t, h, "/admin/products/publish-records?project=p1&status=active&keyword=%E5%95%86")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("成功档应为 200，实际 %d，body=%s", rec.Code, headOf(body))
	}
	for _, want := range []string{
		"已上线商品", "有实例没上线", "没建实例",
		"/products/live",                     // 线上路径
		"v7",                                 // 产物版本
		"2026-10-20",                         // 发布时间（formatTime 口径）
		"/api/product/detail-template/apply", // 重新发布的写入口
		"待重建",                                // stale 徽标
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q：%s", want, headOf(body))
		}
	}
	// 没有实例的行不给「重新发布」：按模板再发一次无处可发。
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

// 端口未装配：页面照常 200，给一句「还没接入」—— 不是「一个都没发布」，也不是 500。
func TestProductPublishRecordPageNotWiredRendersHint(t *testing.T) {
	view := productPublishRecordPageViewStub(func(v *productdto.ProductPublishRecordPageResp) {
		v.Ready = false
		v.Projects = []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}
		v.SelectedProject = "p1"
	})
	reader := &productPageReaderStub{publishView: view}
	h := &ProductPageHandle{reader: reader, products: reader}

	rec := publishRecordRequest(t, h, "/admin/products/publish-records?project=p1")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("未装配档也应是 200（提示而非错误），实际 %d，body=%s", rec.Code, headOf(body))
	}
	if !strings.Contains(body, "发布能力还没有接入这一屏") {
		t.Fatalf("缺少未装配提示：%s", headOf(body))
	}
}

// 读模型失败：同页完整 HTML + 500 + no-store（与其它整页 GET 同档）。
func TestProductPublishRecordPageLoadFailureIsJSON500(t *testing.T) {
	reader := &productPageReaderStub{publishErr: errors.New("驱动：读发布登记失败")}
	h := &ProductPageHandle{reader: reader, products: reader}

	rec := publishRecordRequest(t, h, "/admin/products/publish-records?project=p1")
	assertProductJSONFailure(t, "发布记录页主数据失败", rec, http.StatusInternalServerError, "内部错误")
}
