// Package feature product 模块 feature 测试 —— 后台商品页的评分维护（issue #33）。
//
// 覆盖：
//
//	· 页面渲染评分区块（有评分时显示投影出的平均分与条数 + 明细；无评分时是明确的空态）；
//	· 添加 / 删除动作走真实的页面表单链路（302 回**该商品的详情页**，再渲染能看到变化）；
//	· 非法分值被拒且**不落库**（0~5 之外、非数字）。
//
// 用真实 Jet 模板渲染（与生产同一 template root），断言的是「页面里到底有没有那几样东西」。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
)

// newProductsPageEngine 装配一个只挂商品页与评分动作的测试引擎（真实 Jet 模板 + 真实 service）。
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
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	engine.GET("/admin/products", handle.ProductsPage)
	// 评分是商品的子资源：明细表与「添加评分」入口在商品详情页。
	engine.GET("/admin/products/detail", handle.ProductDetailPage)
	engine.POST("/admin/products/rating/add", handle.ProductsRatingAdd)
	engine.POST("/admin/products/rating/delete", handle.ProductsRatingDelete)
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

// productsPageBody 取商品页 HTML（可选带上错误回显查询串）。
func productsPageBody(t *testing.T, engine *gin.Engine, projectID, extra string) string {
	t.Helper()
	path := "/admin/products?project=" + projectID + extra
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("商品页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// postRatingForm 提交一个评分表单并断言 302，返回 Location。
//
// 底层的 postForm（返回 recorder）在商品属性页测试里已有，这里只加「必须重定向」这一层断言。
func postRatingForm(t *testing.T, engine *gin.Engine, path string, form url.Values) string {
	t.Helper()
	rec := postForm(engine, path, form)
	if rec.Code != http.StatusFound {
		t.Fatalf("表单提交应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
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
	_ = unrated
	for _, score := range []float64{4, 5} {
		if _, err := f.products.AddRating(ctx, &productdto.AddRatingReq{ProductID: rated, Score: score}); err != nil {
			t.Fatalf("写入评分失败: %v", err)
		}
	}

	// 有评分：徽章里的 4.50 · 2 条是**投影算出来的**，不是任何列上存着的值。
	// 详情页一次只渲染一个商品的子资源，所以有评分 / 无评分要分别请求。
	body := productDetailBody(t, engine, f.projectID, rated)
	for _, want := range []string{"4.50", "2 条", "添加评分", "products/rating/add", "products/rating/delete"} {
		if !strings.Contains(body, want) {
			t.Fatalf("有评分的商品详情页应包含 %q", want)
		}
	}
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

// TestProductRatingPageAddAndDelete 添加与删除走真实表单链路：写入后页面能看到，删除后消失。
func TestProductRatingPageAddAndDelete(t *testing.T) {
	engine, f := newProductsPageEngine(t)
	if engine == nil {
		return
	}
	product := f.createProduct(t, "待评分货", "rating-crud", "", 99, 99)

	loc := postRatingForm(t, engine, "/admin/products/rating/add", url.Values{
		"projectId": {f.projectID}, "productId": {product}, "score": {"4.5"},
	})
	if loc != detailLocation(f.projectID, product) {
		t.Fatalf("加评分后应留在该商品的详情页，实际 Location=%q", loc)
	}
	body := productDetailBody(t, engine, f.projectID, product)
	if !strings.Contains(body, "4.50") || !strings.Contains(body, "1 条") {
		t.Fatalf("添加后详情页应显示 4.50 · 1 条")
	}

	// 取一条明细的 id 删掉它。
	res, err := f.products.ListRatings(context.Background(), &productdto.ListRatingsReq{ProductID: product})
	if err != nil || len(res.Items) != 1 {
		t.Fatalf("应有 1 条评分明细：%v %+v", err, res)
	}
	// 页面表单里 productId 与 id（评分 id）是一起提交的：id 是评分 id，回跳的商品靠 productId。
	loc = postRatingForm(t, engine, "/admin/products/rating/delete", url.Values{
		"projectId": {f.projectID}, "productId": {product}, "id": {res.Items[0].ID},
	})
	if loc != detailLocation(f.projectID, product) {
		t.Fatalf("删评分后应留在该商品的详情页，实际 Location=%q", loc)
	}
	body = productDetailBody(t, engine, f.projectID, product)
	if !strings.Contains(body, "这个商品还没有评分。") {
		t.Fatalf("删除后该商品应回到「还没有评分」空态")
	}
}

// TestProductRatingPageRejectsInvalidScore 0~5 之外与非数字一律拒绝，且**不落库**。
func TestProductRatingPageRejectsInvalidScore(t *testing.T) {
	engine, f := newProductsPageEngine(t)
	if engine == nil {
		return
	}
	product := f.createProduct(t, "非法评分货", "rating-invalid", "", 99, 99)
	ctx := context.Background()

	for _, bad := range []string{"9", "-1", "abc", ""} {
		loc := postRatingForm(t, engine, "/admin/products/rating/add", url.Values{
			"projectId": {f.projectID}, "productId": {product}, "score": {bad},
		})
		// 失败也留在详情页（用户就在这一页操作），只把错误经 ?err= 带回。
		if !strings.HasPrefix(loc, detailLocation(f.projectID, product)) {
			t.Fatalf("非法分值 %q 应回该商品的详情页，实际 %s", bad, loc)
		}
		if !strings.Contains(loc, "err=") {
			t.Fatalf("非法分值 %q 应带错误回显，实际 %s", bad, loc)
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
