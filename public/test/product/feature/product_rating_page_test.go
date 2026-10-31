package feature

import (
	"context"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
)

func newProductsPageEngine(t *testing.T) (*gin.Engine, *detailFixture) {
	t.Helper()
	f := newDetailFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	api := producthttp.NewProductHandle(f.products)

	engine.GET("/admin/products/detail", pageview.UseRenderer(adminui.RenderPage), handle.ProductDetailPage)
	engine.GET("/admin/products/edit", pageview.UseRenderer(adminui.RenderPage), handle.ProductEditPage)
	engine.POST("/api/product/rating/add", api.AddRating)
	engine.POST("/api/product/rating/delete", api.DeleteRating)
	return engine, f
}

func productDetailBody(t *testing.T, engine *gin.Engine, projectID, productID string) string {
	t.Helper()
	path := "/admin/products/detail?project=" + projectID + "&product=" + productID
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("商品详情页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

type jsonEnvelope struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func decodeJSONEnvelope(t *testing.T, rec *httptest.ResponseRecorder) jsonEnvelope {
	t.Helper()
	var env jsonEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是 JSON（HTTP %d）：%s", rec.Code, rec.Body.String())
	}
	return env
}

func TestProductRatingPageShowsDetailAndProjection(t *testing.T) {
	engine, f := newProductsPageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	rated := f.createProduct(t, "有评分货", "rated-page", "", 99, 99)
	unrated := f.createProduct(t, "无评分货", "unrated-page", "", 99, 99)
	for _, score := range []float64{4, 5} {
		if _, err := f.products.AddRating(ctx, &productdto.AddRatingReq{ProductID: rated, Score: &score}); err != nil {
			t.Fatalf("写入评分失败: %v", err)
		}
	}

	body := productDetailBody(t, engine, f.projectID, rated)
	for _, want := range []string{`<span data-product-rating="4.5">4.5</span>`, "2 条"} {
		if !strings.Contains(body, want) {
			t.Fatalf("有评分的商品详情页应包含 %q", want)
		}
	}

	editBody := getProductEditPage(engine, f.projectID, rated)
	for _, want := range []string{
		`<span data-product-rating="4.5">4.5</span>`, "2 条", "添加评分",
		`data-write-url="/api/product/rating/add"`,
		`data-write-url="/api/product/rating/delete"`,
	} {
		if !strings.Contains(editBody, want) {
			t.Fatalf("有评分的商品编辑页应包含 %q", want)
		}
	}
	if strings.Contains(body, "添加评分") || strings.Contains(body, "api/product/rating/add") {
		t.Fatalf("详情页是只读页，不该出现评分增删入口")
	}
	assertRatingFormsInsideHost(t, editBody)

	if !strings.Contains(body, `<td><span data-product-rating="4">4</span></td>`) || !strings.Contains(body, `<td><span data-product-rating="5">5</span></td>`) {
		t.Fatalf("页面应列出每条评分的分值")
	}

	unratedBody := productDetailBody(t, engine, f.projectID, unrated)
	if !strings.Contains(unratedBody, "这个商品还没有评分。") {
		t.Fatalf("无评分商品应显示明确的空态文案")
	}
	if strings.Contains(unratedBody, `data-product-rating=`) {
		t.Fatalf("无评分不该被渲染成 0.00（没有评分 ≠ 0 分）")
	}
}

func assertRatingFormsInsideHost(t *testing.T, body string) {
	t.Helper()
	deleteForm := strings.Index(body, `data-write-url="/api/product/rating/delete"`)
	if deleteForm < 0 {
		t.Fatalf("编辑页缺少评分删除表单")
	}
	host := strings.LastIndex(body[:deleteForm], "data-write-host")
	sectionEnd := strings.LastIndex(body[:deleteForm], "</section>")
	if host < 0 || host < sectionEnd {
		t.Fatalf("评分删除表单必须在评分卡片的 [data-write-host] 内，否则失败原因落不到结果槽")
	}
}

func TestProductRatingAddAndDeleteViaJSON(t *testing.T) {
	engine, f := newProductsPageEngine(t)
	if engine == nil {
		return
	}
	product := f.createProduct(t, "待评分货", "rating-crud", "", 99, 99)

	rec := postJSON(t, engine, "/api/product/rating/add", map[string]any{
		"projectId": f.projectID, "productId": product, "score": 4.5,
	})
	if env := decodeJSONEnvelope(t, rec); env.Code != http.StatusOK {
		t.Fatalf("添加评分应成功，实际 HTTP %d code=%d message=%s", rec.Code, env.Code, env.Message)
	}
	body := productDetailBody(t, engine, f.projectID, product)
	if !strings.Contains(body, `<span data-product-rating="4.5">4.5</span>`) || !strings.Contains(body, "1 条") {
		t.Fatalf("添加后详情页应显示 4.50 · 1 条")
	}

	res, err := f.products.ListRatings(context.Background(), &productdto.ListRatingsReq{ProductID: product})
	if err != nil || len(res.Items) != 1 {
		t.Fatalf("应有 1 条评分明细：%v %+v", err, res)
	}
	rec = postJSON(t, engine, "/api/product/rating/delete", map[string]any{
		"projectId": f.projectID, "id": res.Items[0].ID,
	})
	if env := decodeJSONEnvelope(t, rec); env.Code != http.StatusOK {
		t.Fatalf("删除评分应成功，实际 HTTP %d code=%d message=%s", rec.Code, env.Code, env.Message)
	}
	body = productDetailBody(t, engine, f.projectID, product)
	if !strings.Contains(body, "这个商品还没有评分。") {
		t.Fatalf("删除后该商品应回到「还没有评分」空态")
	}
}

func TestProductRatingPageRejectsInvalidScore(t *testing.T) {
	engine, f := newProductsPageEngine(t)
	if engine == nil {
		return
	}
	product := f.createProduct(t, "非法评分货", "rating-invalid", "", 99, 99)
	ctx := context.Background()

	for _, bad := range []any{9, -1, "abc", ""} {
		rec := postJSON(t, engine, "/api/product/rating/add", map[string]any{
			"projectId": f.projectID, "productId": product, "score": bad,
		})
		env := decodeJSONEnvelope(t, rec)
		if env.Code == http.StatusOK {
			t.Fatalf("非法分值 %v 应被拒绝，实际成功：%s", bad, rec.Body.String())
		}

		if strings.TrimSpace(env.Message) == "" {
			t.Fatalf("非法分值 %v 应带原因，body=%s", bad, rec.Body.String())
		}
	}

	res, err := f.products.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: product})
	if err != nil {
		t.Fatalf("读评分失败: %v", err)
	}
	if len(res.Items) != 0 || res.HasRating {
		t.Fatalf("非法评分不该落库，实际 %+v", res)
	}
}
