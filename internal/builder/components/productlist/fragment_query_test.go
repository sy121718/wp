package productlist

import (
	"go_wp/internal/builder/core"
	"net/url"
	"strings"
	"testing"
)

func TestCatalogQueryOnlyDataParameters(t *testing.T) {
	p := Props{TitleField: "item.name", PriceField: "item.priceRange", LinkField: "item.url", Currency: "¥", PageSize: 12, Filters: "categories", FilterCategoryID: "11111111-1111-1111-1111-111111111111"}
	raw := catalogQuery("list-1", &p, &core.RenderContext{ProjectID: "proj-1", Lang: "en-US"})
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 6 || q.Get("projectId") != "proj-1" || q.Get("lang") != "en-US" || !strings.Contains(q.Get("filter"), "categoryId") {
		t.Fatalf("数据查询形状错误: %v", q)
	}
	for _, key := range []string{"nodeId", "titleField", "priceField", "currency", "layout", "columns", "template", "props"} {
		if q.Has(key) {
			t.Fatalf("请求泄露作者展示配置 %s", key)
		}
	}
}
