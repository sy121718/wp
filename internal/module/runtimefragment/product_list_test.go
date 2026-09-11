package runtimefragment

// product_list_test.go — 商品列表片段单测（issue #27）。
//
// 关键断言是**参数 → 筛选下推 → 渲染**这条链路：片段不自己拼 HTML，而是把 URL 参数还原成
// 组件节点后调 builder.RenderNodeHTML（与静态产物同一份渲染），所以这里验证的是
// 「参数确实变成了组件 props、再变成了集合源的过滤条件」。

import (
	"context"
	"strings"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
)

// stubCollection 固定集合项的桩解析器；记录最后一次的源与过滤条件。
type stubCollection struct {
	items  []map[string]any
	source string
	filter map[string]string
}

func (s *stubCollection) ResolveCollection(_ context.Context, source string, filter map[string]string) ([]map[string]any, error) {
	s.source, s.filter = source, filter
	return s.items, nil
}

// listItem 一条商品集合项（字段形状与真实集合项一致）。
func listItem(slug, name string) map[string]any {
	return map[string]any{
		"id": idOf(slug), "name": name, "slug": slug,
		"images":     []any{"/storage/" + slug + ".jpg"},
		"priceRange": "99 ~ 199",
		"tags":       `["新品"]`,
	}
}

func idOf(slug string) string { return "id-" + slug }

// TestRenderProductListFragment 参数 → props → 过滤条件 → HTML 的完整链路。
func TestRenderProductListFragment(t *testing.T) {
	stub := &stubCollection{items: []map[string]any{listItem("summer-shirt", "夏季衬衫")}}
	SetCollectionResolver(stub)
	defer SetCollectionResolver(nil)

	out, err := renderProductList(context.Background(), &Request{
		Type: "productList",
		Params: map[string]string{
			"nodeId": "list-1", "projectId": "proj-1",
			"titleField": "item.name", "priceField": "item.priceRange",
			"linkField": "item.slug", "linkPrefix": "/products/",
			"categoryId":   "11111111-1111-1111-1111-111111111111",
			"option.color": "red",
			"option.size":  "m",
		},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "夏季衬衫") || !strings.Contains(out, "sky-product-list-items") {
		t.Fatalf("片段应渲染出商品卡结构\n%s", out)
	}
	if !strings.Contains(out, "/products/summer-shirt") {
		t.Fatalf("链接应拼上前缀\n%s", out)
	}

	// 过滤条件：等值维度直传、属性维度收成 filterOptions 后仍以 option.<key> 下推、
	// 并且**强制 status=published**（片段不越权读未发布内容）。
	if stub.source != productcontract.CollectionSourceProduct {
		t.Fatalf("集合源应为 %s，实际 %q", productcontract.CollectionSourceProduct, stub.source)
	}
	if stub.filter["status"] != productenums.StatusPublished {
		t.Fatalf("片段必须强制只出已发布，实际 %v", stub.filter)
	}
	if stub.filter["categoryId"] == "" {
		t.Fatalf("分类维度应下推: %v", stub.filter)
	}
	if stub.filter["option.color"] != "red" || stub.filter["option.size"] != "m" {
		t.Fatalf("多属性维度应逐项下推: %v", stub.filter)
	}
}

// TestRenderProductListFragmentRejectsBadField 字段槽位必须过商品字段白名单
// （构建期保存校验管不到运行期请求，这一层是运行期的等价防线）。
func TestRenderProductListFragmentRejectsBadField(t *testing.T) {
	SetCollectionResolver(&stubCollection{})
	defer SetCollectionResolver(nil)
	_, err := renderProductList(context.Background(), &Request{
		Params: map[string]string{
			"nodeId": "list-1", "projectId": "proj-1",
			"titleField": "item.bogusField",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("越界字段应被拒绝，实际 %v", err)
	}
}

// TestRenderProductListFragmentRequiresContext 缺依赖与缺参数都要明确报错。
func TestRenderProductListFragmentRequiresContext(t *testing.T) {
	SetCollectionResolver(nil)
	if _, err := renderProductList(context.Background(), &Request{Params: map[string]string{"nodeId": "n"}}); err == nil {
		t.Fatalf("集合解析器未接入应报错")
	}
	SetCollectionResolver(&stubCollection{})
	defer SetCollectionResolver(nil)
	if _, err := renderProductList(context.Background(), &Request{Params: map[string]string{}}); err == nil {
		t.Fatalf("缺 nodeId 应报错")
	}
	if _, err := renderProductList(context.Background(), &Request{Params: map[string]string{"nodeId": "n"}}); err == nil {
		t.Fatalf("缺 projectId 应报错")
	}
}
