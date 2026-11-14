package feature

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/pkg/auth"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"strings"
)

// 商品、库存与审计走真实迁移库；只在夹具中授予权限，不装配生产登录。
func TestProductHTMLRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 运行商品真实 PostgreSQL/Gin/Jet/Chrome 验收")
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = auth.Close() })
	// 该夹具不走 routers 装配，需接入同一页面事实端口，令牌仍由真实会话签发。
	pageview.SetAdminPort(pageview.AdminPort{CSRFToken: func(c *gin.Context) string {
		token, err := builtin.EnsureCSRFToken(c)
		if err != nil {
			t.Errorf("签发浏览器 CSRF 令牌失败：%v", err)
		}
		return token
	}})
	t.Cleanup(func() { pageview.SetAdminPort(pageview.AdminPort{}) })
	engine, f := newCreateFlowEngine(t, builtin.CSRFMiddleware(), func(c *gin.Context) {
		if c.Request.Method == "POST" && c.GetHeader("X-Product-Test-Delay") == "1" {
			time.Sleep(350 * time.Millisecond)
		}
		c.Next()
	})
	if engine == nil {
		t.Fatal("商品浏览器专项必须使用真实 PostgreSQL")
	}
	inventory := inventoryservice.NewService(inventorymodel.NewModel(f.db), f.projects)
	f.svc.SetInventoryService(inventory)
	inventory.SetVariantWarehouseStocks(f.svc)
	for _, code := range []string{"SZ", "SH"} {
		if _, err := inventory.CreateWarehouse(t.Context(), &inventorydto.CreateWarehouseReq{ProjectID: f.projectID, Name: code + " 测试仓", Code: code}); err != nil {
			t.Fatal(err)
		}
	}
	color := mkVariationAttr(t, f, "颜色", "browser-color", []string{"red", "blue"})
	size := mkVariationAttr(t, f, "尺寸", "browser-size", []string{"s", "m"})
	editIDs := map[int]string{}
	editSKUs := map[int]string{}
	for _, width := range []int{1440, 768, 375} {
		price := 12.5
		p, err := f.svc.Create(t.Context(), &productdto.CreateReq{
			ProjectID: f.projectID, Name: fmt.Sprintf("browser-edit-%d", width), Slug: fmt.Sprintf("browser-edit-%d", width),
			DefaultPrice: &price, Weight: &price, AttributeIDs: []string{color.ID, size.ID},
			Description: json.RawMessage(`{"html":"<p>原始富文本</p>"}`),
			Images:      []string{testMediaBaseURL + "/img/one.png", testMediaBaseURL + "/img/two.png"}, ImageAlts: []string{"", "第二张"},
		})
		if err != nil {
			t.Fatal(err)
		}
		editIDs[width], editSKUs[width] = p.ID, p.Variants[0].SKUCode
		for _, kind := range []string{"single", "bulk-first", "bulk-second"} {
			slug := fmt.Sprintf("browser-%s-%d", kind, width)
			if _, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: slug, Slug: slug}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "已有商品", Slug: "browser-conflict"}); err != nil {
		t.Fatal(err)
	}
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	handle.SetInventoryDeps(inventory)
	engine.GET("/admin/products/edit", pageview.UseRenderer(pageview.RenderPage), handle.ProductEditPage)
	mountStaticAssets(engine)
	server := httptest.NewServer(engine)
	defer server.Close()
	assertStaticAssetsServed(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "testdata/product-html-browser.cjs")
	ids, err := json.Marshal(editIDs)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "GOWP_PRODUCT_BASE_URL="+server.URL, "GOWP_PRODUCT_PROJECT_ID="+f.projectID, "GOWP_PRODUCT_EDIT_IDS="+string(ids), "GOWP_PUPPETEER_MODULE="+browserPuppeteerModule())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("商品真实浏览器验收失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
	result, err := f.svc.QueryProducts(t.Context(), &productdto.ListReq{ProjectID: f.projectID, Page: 1, Size: 20})
	if err != nil || result.Total != 7 {
		t.Fatalf("仅应留下已有商品、三次新建与三个编辑样本：%v %+v", err, result)
	}
	for _, tc := range []struct {
		width int
		price *float64
	}{{1440, nil}, {768, new(float64)}, {375, func() *float64 { v := 12.5; return &v }()}} {
		rows, err := f.svc.QueryProducts(t.Context(), &productdto.ListReq{ProjectID: f.projectID, Keyword: fmt.Sprintf("browser-created-%d", tc.width), Page: 1, Size: 20})
		if err != nil || rows.Total != 1 {
			t.Fatalf("浏览器创建必须恰好落库一次：%v %+v", err, rows)
		}
		p, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: rows.Items[0].ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.AttributeIDs) != 2 || len(p.Variants) != 1 || (tc.price == nil) != (p.DefaultPrice == nil) || tc.price != nil && *p.DefaultPrice != *tc.price {
			t.Fatalf("浏览器实际提交的属性、默认变体或可选金额不符：%+v", p)
		}
		edited, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: editIDs[tc.width]})
		if err != nil {
			t.Fatal(err)
		}
		var description struct {
			HTML string `json:"html"`
		}
		if err := json.Unmarshal(edited.Description, &description); err != nil {
			t.Fatal(err)
		}
		if edited.Name != fmt.Sprintf("browser-edited-%d", tc.width) || !strings.Contains(description.HTML, fmt.Sprintf("浏览器富文本-%d", tc.width)) ||
			len(edited.AttributeIDs) != 0 || len(edited.Images) != 2 || len(edited.ImageAlts) != 2 || edited.ImageAlts[0] != "" || edited.ImageAlts[1] != fmt.Sprintf("浏览器说明-%d", tc.width) ||
			len(edited.Variants) != 1 || edited.Variants[0].SKUCode != editSKUs[tc.width] || edited.Variants[0].Price != 12.5 {
			t.Fatalf("编辑数据库终态必须保留真实富文本、空alt和未提交变体：%+v description=%s", edited, description.HTML)
		}
		if edited.Rating.HasRating || edited.Rating.RatingCount != 0 || len(edited.Rating.Items) != 0 {
			t.Fatalf("浏览器评分增删必须真实落库并最终为空：%+v", edited.Rating)
		}
		if tc.width == 1440 {
			if edited.DefaultPrice != nil || edited.Weight != nil {
				t.Fatalf("浏览器明确清空应写为 NULL：%+v", edited)
			}
		} else {
			price, weight := 0.0, 0.0
			if tc.width == 375 {
				price, weight = 23.5, 1.25
			}
			if edited.DefaultPrice == nil || *edited.DefaultPrice != price || edited.Weight == nil || *edited.Weight != weight {
				t.Fatalf("浏览器数值零与实际金额/重量必须准确落库：%+v", edited)
			}
		}
	}
}
