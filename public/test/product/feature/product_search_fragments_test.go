package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	contentcontract "go_wp/internal/module/content/contract"
	contenthttp "go_wp/internal/module/content/inbound/http"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// seedArticle 造一篇文章（contents 表；标题与摘要在 data JSONB 里）。
func seedArticle(t *testing.T, f *detailFixture, title, slug, excerpt string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"title": title, "excerpt": excerpt})
	if err != nil {
		t.Fatalf("文章内容编码失败: %v", err)
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	if err := contentmodel.NewModel(f.db).Create(context.Background(), &contentmodel.Entity{
		ID: id, EntityType: "article", Slug: slug, Revision: 1,
		Data: raw, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写文章失败: %v", err)
	}
	return id
}

// searchJSON 走生产公开JSON路由；不用片段注册表或展示模板。
func searchJSON(t *testing.T, f *detailFixture, q string) contentcontract.PublicSearchData {
	t.Helper()
	contents := contentservice.NewService(contentmodel.NewModel(f.db))
	reader := contentservice.NewPublicSearchService(contents, f.products, f.pres)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	contenthttp.SetupContentPublicRoutes(engine, reader)
	rec := httptest.NewRecorder()
	params := url.Values{"projectId": {f.projectID}, "q": {q}}
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/site/search?"+params.Encode(), nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("搜索必须返回JSON: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data contentcontract.PublicSearchData `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, html := range []string{"<ul", "<li", "没有找到与", "请输入搜索"} {
		if strings.Contains(rec.Body.String(), html) {
			t.Fatalf("搜索数据混入展示成品 %q", html)
		}
	}
	return body.Data
}

func TestSearchJSONOnlyPublishedPaths(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := f.createProduct(t, "帆布鞋", "canvas-shoes", "轻便透气", 129, 199)
	status := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: id, Status: &status}); err != nil {
		t.Fatal(err)
	}
	inst := f.publish(t, id, "/real/shoes")
	noPage := f.createProduct(t, "帆布鞋没有详情", "canvas-no-page", "", 1, 2)
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: noPage, Status: &status}); err != nil {
		t.Fatal(err)
	}
	f.createProduct(t, "帆布鞋草稿", "canvas-draft", "", 1, 2)
	seedArticle(t, f, "帆布鞋护理指南", "shoe-care", "未发布文章")
	out := searchJSON(t, f, "帆布鞋")
	if len(out.Products) != 1 || out.Products[0].ID != id || !strings.HasSuffix(out.Products[0].URL, inst.URLPath) || len(out.Articles) != 0 {
		t.Fatalf("公开搜索泄露隐藏或猜URL: %+v", out)
	}
	for _, q := range []string{"不存在的关键词", "   "} {
		empty := searchJSON(t, f, q)
		if len(empty.Articles) != 0 || len(empty.Products) != 0 {
			t.Fatalf("空态只能交空事实: %+v", empty)
		}
	}
}

func TestSearchJSONEscapesLikeWildcards(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	status := productenums.StatusPublished
	for _, product := range []struct{ name, slug string }{{"100 元包邮小样", "sample-100"}, {"纯棉 100% 毛巾", "towel-100"}} {
		id := f.createProduct(t, product.name, product.slug, "吸水", 39, 59)
		if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: id, Status: &status}); err != nil {
			t.Fatal(err)
		}
		f.publish(t, id, "/real/"+product.slug)
	}
	out := searchJSON(t, f, "100%")
	if len(out.Products) != 1 || out.Products[0].Name != "纯棉 100% 毛巾" {
		t.Fatalf("LIKE必须字面匹配: %+v", out)
	}
}
