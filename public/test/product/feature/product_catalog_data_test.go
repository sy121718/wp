package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	json "go_wp/pkg/jsonx"
)

// 真实发布→单语言Publication索引→公开JSON，计数和数据都排除隐藏/跨工程/跨语言项。
func TestPublicCatalogFactsAndPaging(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	a := f.createProduct(t, "50% 纯棉", "catalog-a", "", 129, 199)
	b := f.createProduct(t, "50xyz 棉布", "catalog-b", "", 59, 99)
	unpublished := f.createProduct(t, "无详情商品", "catalog-hidden", "", 1, 2)
	draft := f.createProduct(t, "草稿详情商品", "catalog-draft", "", 1, 2)
	published := productenums.StatusPublished
	for _, id := range []string{a, b, unpublished} {
		if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: id, Status: &published, Images: []string{"https://cdn.example.test/catalog.jpg"}}); err != nil {
			t.Fatal(err)
		}
	}
	f.publish(t, a, "/real/catalog-a")
	f.publish(t, b, "/real/catalog-b")
	f.publish(t, draft, "/real/catalog-draft")
	f.products.SetPublicCatalogIndex(f.pres)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	producthttp.SetupProductCatalogRoutes(engine, f.products)
	read := func(q url.Values, status int) productdto.PublicCatalogResp {
		t.Helper()
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/site/product/catalog?"+q.Encode(), nil))
		if rec.Code != status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("响应错误 %d %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Code    int                          `json:"code"`
			Message string                       `json:"message"`
			Data    productdto.PublicCatalogResp `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, word := range []string{"costPrice", "CostPrice", "<ul", "<li", "pageText", "emptyText"} {
			if strings.Contains(rec.Body.String(), word) {
				t.Fatalf("数据接口混入内部/展示形状 %s: %s", word, rec.Body.String())
			}
		}
		if status >= 400 && body.Message == "" {
			t.Fatal("参数错误缺少归口文案")
		}
		return body.Data
	}
	query := url.Values{"projectId": {f.projectID}, "sort": {"priceAsc"}, "pageSize": {"1"}}
	first := read(query, 200)
	query.Set("page", "2")
	second := read(query, 200)
	if first.Total != 2 || second.Total != first.Total || first.Page != 1 || second.Page != 2 || len(first.Items) != 1 || len(second.Items) != 1 {
		t.Fatalf("数据/计数分页口径漂移: %+v %+v", first, second)
	}
	if !reflect.DeepEqual([]string{first.Items[0].ID, second.Items[0].ID}, []string{b, a}) {
		t.Fatalf("价格排序未先于分页: %+v %+v", first.Items, second.Items)
	}
	item := second.Items[0]
	if item.Price == nil || *item.Price != 12900 || item.MaxPrice == nil || *item.MaxPrice != 19900 || !strings.HasSuffix(item.URL, "/real/catalog-a") || item.Images[0] != "https://cdn.example.test/catalog.jpg" {
		t.Fatalf("金额或真实路径事实错误: %+v", item)
	}
	query.Set("page", "99")
	if out := read(query, 200); out.Total != first.Total || len(out.Items) != 0 {
		t.Fatalf("越界页不能伪造总数: %+v", out)
	}
	query.Del("page")
	query.Set("q", "50%")
	if out := read(query, 200); out.Total != 1 || out.Items[0].ID != a {
		t.Fatalf("LIKE通配符必须按字面检索: %+v", out)
	}
	query.Del("q")
	query.Set("filter", `{"minPrice":"100"}`)
	if out := read(query, 200); out.Total != 1 || out.Items[0].ID != a {
		t.Fatalf("筛选必须在计数与分页前下推: %+v", out)
	}
	query.Set("filter", `{"arbitrary":"x"}`)
	read(query, 400)
	query.Set("filter", "null")
	query.Set("lang", "zz-ZZ")
	if out := read(query, 200); out.Total != 0 || len(out.Items) != 0 {
		t.Fatalf("无目标语言Publication不能回退逻辑路径: %+v", out)
	}
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一公开工程"})
	if err != nil {
		t.Fatal(err)
	}
	query.Del("lang")
	query.Set("projectId", other.ID)
	if out := read(query, 200); out.Total != 0 || len(out.Items) != 0 {
		t.Fatalf("空候选不能退成全库: %+v", out)
	}
	query.Set("projectId", f.projectID)
	query.Set("pageSize", "61")
	read(query, 400)
	query.Set("projectId", "invalid")
	read(query, 400)
}
