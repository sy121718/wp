// Package feature product 模块 feature 测试 —— 后台商品页的评分维护（issue #33）。
//
// 覆盖：
//
//	· 页面渲染评分区块（有评分时显示投影出的平均分与条数 + 明细；无评分时是明确的空态）；
//	· 添加 / 删除各打一次 JSON 端点（编辑页的表单已由 ui/json_write.js 接管成 JSON 请求），
//	  再渲染页面能看到变化；
//	· 非法分值被拒且**不落库**（0~5 之外、非数字、空）。
//
// 用真实 Jet 模板渲染（与生产同一 template root），断言的是「页面里到底有没有那几样东西」。
package feature

import (
	"context"
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

// newProductsPageEngine 装配只挂商品详情页 / 编辑页与评分 JSON 端点的测试引擎
// （真实 Jet 模板 + 真实 service）。
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
	handle := producthttp.NewProductPageHandle(f.products, f.products, f.projects)
	api := producthttp.NewHandle(f.products)
	// 评分是商品的子资源：明细表在**只读**的详情页，「添加评分 / 删除」在编辑页，
	// 两者都经由 JSON 端点落库（页面表单由基座接管，不再有 /admin/products/rating/*）。
	engine.GET("/admin/products/detail", handle.ProductDetailPage)
	engine.GET("/admin/products/edit", handle.ProductEditPage)
	engine.POST("/api/product/rating/add", api.AddRating)
	engine.POST("/api/product/rating/delete", api.DeleteRating)
	return engine, f
}

// productDetailBody 取某个商品的详情页 HTML。
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

// jsonEnvelope 是 pkg/response 的外壳：基座按 code >= 400 判失败，200 即成功。
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

// TestProductRatingPageShowsDetailAndProjection 页面渲染评分：
// 有评分时显示**投影算出的**平均分与条数 + 逐条明细；没有评分时是明确的空态文案。
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

	// 有评分：徽章里的 4.50 · 2 条是**投影算出来的**，不是任何列上存着的值。
	// 详情页一次只渲染一个商品的子资源，所以有评分 / 无评分要分别请求。
	body := productDetailBody(t, engine, f.projectID, rated)
	for _, want := range []string{"4.50", "2 条"} {
		if !strings.Contains(body, want) {
			t.Fatalf("有评分的商品详情页应包含 %q", want)
		}
	}
	// 增删入口在编辑页（详情页只读）：入口搬走不等于丢掉，且必须挂在 JSON 写基座上。
	editBody := getProductEditPage(engine, f.projectID, rated)
	for _, want := range []string{
		"4.50", "2 条", "添加评分",
		`data-write-api="/api/product/rating/add"`,
		`data-write-api="/api/product/rating/delete"`,
	} {
		if !strings.Contains(editBody, want) {
			t.Fatalf("有评分的商品编辑页应包含 %q", want)
		}
	}
	if strings.Contains(body, "添加评分") || strings.Contains(body, "api/product/rating/add") {
		t.Fatalf("详情页是只读页，不该出现评分增删入口")
	}
	assertRatingFormsInsideHost(t, editBody)
	// 两条明细各自的分值都在（4.00 与 5.00）。
	if !strings.Contains(body, "4.00") || !strings.Contains(body, "5.00") {
		t.Fatalf("页面应列出每条评分的分值")
	}
	// 无评分的商品显示明确的空态，而不是「0.00 分 / 0 条」—— 这个区分是本页最容易写错的地方。
	unratedBody := productDetailBody(t, engine, f.projectID, unrated)
	if !strings.Contains(unratedBody, "这个商品还没有评分。") {
		t.Fatalf("无评分商品应显示明确的空态文案")
	}
	if strings.Contains(unratedBody, "0.00") {
		t.Fatalf("无评分不该被渲染成 0.00（没有评分 ≠ 0 分）")
	}
}

// assertRatingFormsInsideHost 评分表单必须落在评分卡片的 [data-write-host] 里。
//
// 判据是**位置**而不是「页面上有没有」：基座把失败原因写进最近的 host 结果槽，表单放到卡片外
// 就落不进那个槽 —— 症状是「点了删除没反应」，而所有「存在性」断言都还是绿的。
func assertRatingFormsInsideHost(t *testing.T, body string) {
	t.Helper()
	deleteForm := strings.Index(body, `data-write-api="/api/product/rating/delete"`)
	if deleteForm < 0 {
		t.Fatalf("编辑页缺少评分删除表单")
	}
	host := strings.LastIndex(body[:deleteForm], "data-write-host")
	sectionEnd := strings.LastIndex(body[:deleteForm], "</section>")
	if host < 0 || host < sectionEnd {
		t.Fatalf("评分删除表单必须在评分卡片的 [data-write-host] 内，否则失败原因落不到结果槽")
	}
}

// TestProductRatingAddAndDeleteViaJSON 添加与删除各打一次 JSON 端点：写入后页面能看到，删除后消失。
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
	if !strings.Contains(body, "4.50") || !strings.Contains(body, "1 条") {
		t.Fatalf("添加后详情页应显示 4.50 · 1 条")
	}

	// 取一条明细的 id 删掉它（删除只认评分 id，工程随请求显式带上 —— RLS 作用域）。
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

// TestProductRatingPageRejectsInvalidScore 0~5 之外、非数字与空值一律拒绝，且**不落库**。
//
// 分值在 dto 上是 *float64：0 分是合法评分，不能与「没传」混为一谈。
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
		// 失败要给出能读的原因（越界走「评分必须是 0~5 的数字」，非数字/空值是参数无效）。
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
