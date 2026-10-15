// product_bundle_configurator_scope_test.go — 捆绑配置器片段的工程作用域（P172）。
//
// 缺陷族：调用方手里有工程 id，却没把工程过给下游的取数一跳。
// 片段侧的同族口径是「工程上下文由片段参数给出」（variantAvailability / productList /
// livePrice / searchResults 全部如此），只有 bundleConfigurator 没有这个参数 ——
// 多工程部署下 GetBundleConfig 内部走 resolveProjectID("") 回 ErrInvalidParam，
// 片段端点 500「片段渲染失败」，而正确形态是渲染出这个商品的配置器。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
)

// TestBundleConfiguratorCarriesProjectScope：
// 多工程部署（工程数 ≥ 2）下，配置器片段必须能按片段参数里的工程取数 ——
// 修前这里是 500，因为工程根本没进得去下游。
func TestBundleConfiguratorCarriesProjectScope(t *testing.T) {
	engine, f := newBundleFragmentEngine(t)
	if engine == nil {
		return
	}
	// 第二个工程的存在让「回落唯一工程」这条退路消失（resolveProjectID 的另一个分支）。
	if _, err := f.projects.Create(context.Background(), &projectdto.CreateReq{Name: "第二站点工程"}); err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}
	price := 88.0
	main := f.mkProduct(t, "多工程套餐", "multi-proj-bundle", &price)
	addon := f.mkProduct(t, "多工程子项", "multi-proj-addon", nil)
	v := f.firstVariant(t, addon.ID)
	f.addStock(t, v, 4)
	f.setBundleConfig(t, main.ID, bundleWith(v.ID, true, 1, 1, 0, 1, 0))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/_fragments/bundleConfigurator?productId="+main.ID+"&projectId="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("多工程下配置器片段应 200（工程由片段参数给出），实际 %d body=%s",
			rec.Code, oneLine(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{v.SKUCode, "套餐价", `name="projectId"`, f.projectID} {
		if !strings.Contains(body, want) {
			t.Fatalf("配置器片段缺少 %q\n%s", want, oneLine(body))
		}
	}

	// 缺 projectId 不回落、不猜测：同族口径是「没有工程上下文就报错」。
	miss := httptest.NewRecorder()
	engine.ServeHTTP(miss, httptest.NewRequest(http.MethodGet,
		"/_fragments/bundleConfigurator?productId="+main.ID, nil))
	if miss.Code == http.StatusOK {
		t.Fatalf("缺 projectId 不应成功渲染，实际 200 body=%s", oneLine(miss.Body.String()))
	}

	// 整单校验侧同样要带工程：POST 只收表单字段，工程靠表单隐藏域传递。
	ok := postForm(engine, "/_fragments/bundleConfiguratorCheck", url.Values{
		"productId": {main.ID}, "projectId": {f.projectID}, "variantId": {v.ID}, "qty": {"1"},
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("多工程下整单校验应给出结论片段（200），实际 %d body=%s", ok.Code, oneLine(ok.Body.String()))
	}
	if !strings.Contains(ok.Body.String(), "套餐价") {
		t.Fatalf("合法选择的结论片段应含套餐价，实际 %s", oneLine(ok.Body.String()))
	}
}
