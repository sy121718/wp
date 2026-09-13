// product_search_fragments_test.go — 站内搜索与实时价格核对两个运行时片段（BIZ-2）。
//
// 走**真实**的 runtimefragment 端点 + 真实 PG（生产迁移）+ 真实 service + 真实 Jet 片段模板：
// 断言的是「访问面到底吐出了什么 HTML」，而不是内部状态。四件事必须成立：
//
//	1. 只搜已上架商品与已发布内容 —— 草稿商品、没有线上页面的文章都不得出现；
//	2. 结果链接逐字来自发布面（实例 url_path），没有路径的条目只输出标题，绝不拼 slug 约定；
//	3. LIKE 通配符被转义 —— 搜「100%」不等于「以 100 开头」（不转义时后者会命中）；
//	4. 商品实时价片段：产物里的价与库里一致时沉默，不一致时明确交代当前价。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	runtimefragment "go_wp/internal/module/runtimefragment"
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

// fragmentGet 请求一个匿名片段端点，返回响应体（非 200 直接失败）。
func fragmentGet(t *testing.T, capability string, params url.Values) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	runtimefragment.SetupFragmentRoutes(engine)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_fragments/"+capability+"?"+params.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("片段 %s 应 200，实际 %d body=%s", capability, rec.Code, oneLine(rec.Body.String()))
	}
	return rec.Body.String()
}

// TestSearchResultsFragmentLinksOnlyPublished 搜索片段：只出「真的能看到」的东西。
func TestSearchResultsFragmentLinksOnlyPublished(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 一个已上架商品 + 一个已发布的详情页实例（线上路径由发布面给）。
	published := f.createProduct(t, "帆布鞋", "canvas-shoes", "轻便透气", 129, 199)
	status := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: published, Status: &status}); err != nil {
		t.Fatalf("上架商品失败: %v", err)
	}
	inst := f.publish(t, published, "/products/canvas-shoes")

	// 一个草稿商品（同名关键词，但没上架）。
	f.createProduct(t, "帆布鞋 草稿版", "canvas-draft", "还没上架", 99, 119)

	// 一篇命中关键词的文章，但**没有发布实例** —— 不该出现在结果里。
	seedArticle(t, f, "帆布鞋护理指南", "shoe-care", "怎么洗才不掉色")

	contentSvc := contentservice.NewService(contentmodel.NewModel(f.db))
	runtimefragment.SetContentSearchProvider(contentSvc)
	runtimefragment.SetProductSearchProvider(f.products)
	runtimefragment.SetPublishedEntityLocator(f.pres)
	t.Cleanup(func() {
		runtimefragment.SetContentSearchProvider(nil)
		runtimefragment.SetProductSearchProvider(nil)
		runtimefragment.SetPublishedEntityLocator(nil)
	})

	body := fragmentGet(t, "searchResults", url.Values{"q": {"帆布鞋"}, "projectId": {f.projectID}})
	if !strings.Contains(body, "帆布鞋") {
		t.Fatalf("已上架商品应出现在结果里：%s", oneLine(body))
	}
	wantLink := "href=\"" + inst.URLPath + "\""
	if !strings.Contains(body, wantLink) {
		t.Fatalf("结果链接应逐字来自发布面的线上路径 %q：%s", inst.URLPath, oneLine(body))
	}
	if strings.Contains(body, "草稿版") {
		t.Fatalf("草稿商品不得出现在站内搜索里：%s", oneLine(body))
	}
	if strings.Contains(body, "护理指南") {
		t.Fatalf("没有线上页面的内容不得出现在站内搜索里（未发布内容不外泄）：%s", oneLine(body))
	}

	// 无结果：给一句人话，而不是空块或 500。
	empty := fragmentGet(t, "searchResults", url.Values{"q": {"不存在的关键词"}, "projectId": {f.projectID}})
	if !strings.Contains(empty, "没有找到与") {
		t.Fatalf("无结果应给提示：%s", oneLine(empty))
	}

	// 空关键词：提示而不是报错。
	blank := fragmentGet(t, "searchResults", url.Values{"q": {"   "}, "projectId": {f.projectID}})
	if !strings.Contains(blank, "请输入搜索关键词") {
		t.Fatalf("空关键词应给提示：%s", oneLine(blank))
	}
}

// TestSearchResultsEscapesLikeWildcards LIKE 通配符必须按字面量匹配。
//
// 不转义时「100%」会退化成「以 100 开头」，把「100 元包邮」这类不相关的商品也捞出来 ——
// 用户以为搜到了，其实是搜索在按另一套规则工作。
func TestSearchResultsEscapesLikeWildcards(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 「100 元包邮」不含百分号；搜「100%」不该命中它。
	deco := f.createProduct(t, "100 元包邮小样", "sample-100", "凑单用", 100, 120)
	status := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: deco, Status: &status}); err != nil {
		t.Fatalf("上架商品失败: %v", err)
	}
	// 「纯棉 100%」才是真正含 % 的那个。
	real := f.createProduct(t, "纯棉 100% 毛巾", "towel-100", "吸水", 39, 59)
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: real, Status: &status}); err != nil {
		t.Fatalf("上架商品失败: %v", err)
	}

	runtimefragment.SetProductSearchProvider(f.products)
	runtimefragment.SetPublishedEntityLocator(f.pres)
	t.Cleanup(func() {
		runtimefragment.SetProductSearchProvider(nil)
		runtimefragment.SetPublishedEntityLocator(nil)
	})

	body := fragmentGet(t, "searchResults", url.Values{"q": {"100%"}, "projectId": {f.projectID}})
	if !strings.Contains(body, "纯棉 100% 毛巾") {
		t.Fatalf("含百分号的商品应被搜到：%s", oneLine(body))
	}
	if strings.Contains(body, "100 元包邮小样") {
		t.Fatalf("不含百分号的商品不该被命中（通配符未转义）：%s", oneLine(body))
	}
}

// TestProductLivePriceFragment 实时价格核对片段：一致时沉默、不一致时交代、改价立刻生效。
func TestProductLivePriceFragment(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	productID := f.createProduct(t, "帆布鞋", "canvas-live", "轻便透气", 129, 199)
	detail, err := f.products.Get(ctx, &productdto.GetReq{ID: productID})
	if err != nil || len(detail.Variants) == 0 {
		t.Fatalf("读商品变体失败: %v", err)
	}
	variant := detail.Variants[0]

	runtimefragment.SetVariantSnapshotProvider(f.products)
	t.Cleanup(func() { runtimefragment.SetVariantSnapshotProvider(nil) })

	// ① 产物里的价与库里一致 → 沉默（页面保留产物里的价）。
	body := fragmentGet(t, "productLivePrice", url.Values{
		"variantIds": {variant.ID}, "prices": {"12900"},
	})
	if strings.TrimSpace(body) != "" {
		t.Fatalf("价格一致时应沉默，实际：%s", oneLine(body))
	}

	// ② 产物里的价过期（99 元 vs 当前 129 元）→ 明确交代当前价。
	body = fragmentGet(t, "productLivePrice", url.Values{
		"variantIds": {variant.ID}, "prices": {"9900"},
	})
	if !strings.Contains(body, "价格已更新为 ¥129，以结算为准") {
		t.Fatalf("价不一致时应交代当前价：%s", oneLine(body))
	}

	// ③ 改价只落库、不进构建管线：产物里的价不动，片段下一次请求就看到新价。
	newPrice := 149.0
	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: variant.ID, Price: &newPrice}); err != nil {
		t.Fatalf("改价失败: %v", err)
	}
	body = fragmentGet(t, "productLivePrice", url.Values{
		"variantIds": {variant.ID}, "prices": {"12900"},
	})
	if !strings.Contains(body, "价格已更新为 ¥149，以结算为准") {
		t.Fatalf("改价后片段应给出新价：%s", oneLine(body))
	}

	// ④ 非法 id 形状：丢弃、不 500（否则 PostgreSQL 的 uuid 解析会报 22P02）。
	body = fragmentGet(t, "productLivePrice", url.Values{
		"variantIds": {"not-a-uuid"}, "prices": {"12900"},
	})
	if strings.TrimSpace(body) != "" {
		t.Fatalf("非法 id 应沉默，实际：%s", oneLine(body))
	}
}
