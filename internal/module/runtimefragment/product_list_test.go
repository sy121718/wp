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

// TestRenderProductListFragmentDeterministic 同一参数必须渲染出**逐字节相同**的 HTML。
//
// 这条用例针对的是一个真实存在的坑：筛选参数在 map 里遍历时顺序随机，属性维度若按遍历顺序
// 拼进 filterOptions，两次渲染就会得到不同字节（缓存/CDN 与「点出来的和直接打开的不一样」的源头）。
// 片段里对属性键排序就是为了这个，这里把它钉住。
func TestRenderProductListFragmentDeterministic(t *testing.T) {
	stub := &stubCollection{items: []map[string]any{listItem("summer-shirt", "夏季衬衫")}}
	SetCollectionResolver(stub)
	defer SetCollectionResolver(nil)
	params := map[string]string{
		"nodeId": "list-1", "projectId": "proj-1",
		"titleField": "item.name", "option.color": "red", "option.size": "m",
		"option.material": "cotton",
	}
	first, err := renderProductList(context.Background(), &Request{Params: params})
	if err != nil {
		t.Fatalf("第一次渲染失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		// 每次重建 map：Go 的 map 遍历顺序是随机的，重复用同一个 map 测不出问题。
		again := map[string]string{}
		for k, v := range params {
			again[k] = v
		}
		out, rerr := renderProductList(context.Background(), &Request{Params: again})
		if rerr != nil {
			t.Fatalf("第 %d 次渲染失败: %v", i+2, rerr)
		}
		if out != first {
			t.Fatalf("同参数渲染必须逐字节相同（第 %d 次不同）", i+2)
		}
	}
	// 属性维度必须都下推（排序只是拼装顺序，筛选语义不变）。
	if stub.filter["option.color"] != "red" || stub.filter["option.material"] != "cotton" || stub.filter["option.size"] != "m" {
		t.Fatalf("三个属性维度都该下推: %v", stub.filter)
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
