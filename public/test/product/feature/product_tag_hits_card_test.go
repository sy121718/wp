package feature

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	json "go_wp/pkg/jsonx"
)

func decodeTagProducts(t *testing.T, rec *httptest.ResponseRecorder) *productdto.TagProductsListResp {
	t.Helper()
	var payload struct {
		Data *productdto.TagProductsListResp `json:"data"`
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("命中查询应为 JSON 200：%d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload.Data == nil {
		t.Fatalf("命中事实解码失败：%v %s", err, rec.Body.String())
	}
	return payload.Data
}

// 首屏只给查询入口，商品分页与总数在同一 JSON 契约中返回。
func TestTagPageHitsUsesJSONFacts(t *testing.T) {
	f := newTagQueryFixture(t)
	if f == nil {
		return
	}
	projectID := f.newProject(t, "命中商品 JSON 工程")
	tagID := seedTags(t, f.db, projectID, 1)[0]
	seedTagProducts(t, f.db, projectID, tagID, "命中商品", 60)
	rec := httptestGet(f.engine, "/admin/product-tags?project="+projectID)
	body := rec.Body.String()
	for _, want := range []string{"</html>", `data-product-tag-hits="` + tagID + `"`, `id="tag-hits-panel"`, `aria-live="polite"`} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("标签页缺少 %q：%s", want, body)
		}
	}
	if strings.Contains(body, "命中商品 0000") || strings.Contains(body, "/admin/product-tags/hits") {
		t.Fatal("首屏仍内联商品或请求旧片段")
	}
	seen := map[string]bool{}
	for page := 1; page <= 2; page++ {
		data := decodeTagProducts(t, httptestGet(f.engine, fmt.Sprintf("/api/product/tag/products?projectId=%s&id=%s&page=%d", projectID, tagID, page)))
		want := 50
		if page == 2 {
			want = 10
		}
		if data.Total != 60 || data.Page != page || len(data.Items) != want {
			t.Fatalf("分页事实不符：%+v", data)
		}
		for _, item := range data.Items {
			if seen[item.ID] {
				t.Fatalf("跨页重复商品 %s", item.ID)
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != 60 {
		t.Fatalf("分页覆盖不完整：%d", len(seen))
	}
}
