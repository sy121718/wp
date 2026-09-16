package productlist

// fragment_query_test.go — 实例配置的参数名预算（审计 PERF-019 衍生）。
//
// fragmentQuery 的输出会成为**片段请求的 query**（产物里的 hx-get / href 就是这么来的）。
// 片段端对 GET 的参数名数量有上限（internal/module/runtimefragment/endpoint.go 的
// maxParamCount）：上限必须覆盖「实例配置 + 访客筛选 + 页码」，否则翻页与筛选链接
// 指向的是自己的 400，而这类失败只在真实 HTTP 路径上出现（单测直调处理器看不见）。
//
// 这里钉住**实例配置侧**的规模：它涨上去就得同时调上限，不能只改一边。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestFragmentQueryParamBudget(t *testing.T) {
	p := Props{
		TitleField: "item.name", PriceField: "item.priceRange", LinkField: "item.slug",
		LinkPrefix: "/products/", PageSize: 12, Filters: "categories",
	}
	raw := fragmentQuery("list-1", &p, &core.RenderContext{ProjectID: "proj-1"})
	keys := strings.Split(raw, "&")
	// 预算 = 实例配置允许占用的参数名上限。留出余量给语义参数（筛选维度、option.<key>、page）。
	const budget = 16
	if len(keys) > budget {
		t.Fatalf("实例配置已占 %d 个参数名（预算 %d），片段端上限见 runtimefragment/endpoint.go\n%s",
			len(keys), budget, raw)
	}
	// 空值参数不该写进来：空串一样占一个参数名预算。
	for _, kv := range keys {
		if name, value, ok := strings.Cut(kv, "="); ok && value == "" {
			t.Fatalf("参数 %q 是空值，不应写进实例配置（白占参数名预算）", name)
		}
	}
	t.Logf("实例配置参数名数量 = %d", len(keys))
}
