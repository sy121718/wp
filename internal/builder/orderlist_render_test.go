package builder

// orderlist_render_test.go — 订单列表组件的**产物级**验证。
//
// 组件包的测试覆盖 BuildView；模板对不对只能在编译产物里看：
// Jet 里一个写错的变量通常渲染成空串而不是报错 —— 那正是「组件测试全绿、
// 页面上却什么都没有」的成因。所以这里断言的是产物 HTML 里真的有什么。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// orderListDoc 含一个订单列表节点的最小文档。
func orderListDoc(t *testing.T, props map[string]any) *Page {
	t.Helper()
	doc, err := json.Marshal(map[string]any{
		"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
		"root":     []any{map[string]any{"id": "orders", "type": "core.orderList", "props": props}},
	})
	if err != nil {
		t.Fatalf("序列化文档失败: %v", err)
	}
	page, err := ParsePage(doc)
	if err != nil {
		t.Fatalf("文档解析失败: %v", err)
	}
	return page
}

// TestOrderListCompilesToFragmentMount 产物里必须是一个真能拉片段的容器。
func TestOrderListCompilesToFragmentMount(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := orderListDoc(t, map[string]any{"title": "我的订单记录", "showTitle": true, "pageSize": 5})
	compiled, err := Compile(page,
		WithComponentSet(set),
		WithProjectID("proj-9"),
		WithSitePages(map[string]string{"login": "/login", "orders": "/orders"}),
	)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	for _, want := range []string{
		`hx-get="/_fragments/ordersList?`, // 拉片段的入口
		`hx-trigger="load"`,               // 进页面就拉
		"projectId=proj-9",                // 工程 id 来自构建上下文
		"limit=5",                         // 每页条数真的传下去了
		">我的订单记录<",                        // 作者自定义的标题
		`href="/login"`,                   // 未登录引导的登录链接（无 JS 时也可见）
		`href="/orders"`,                  // 订单页兜底链接
		"登录后可以查看你的订单。",                    // 容器默认内容：无 JS 时这就是访客看到的东西
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("产物缺少 %q；实际产物：%s", want, doc)
		}
	}
}

// TestOrderListWithoutProjectStaysCompilable 缺工程 id 时降级为提示而不是构建失败。
//
// pipeline.DefaultCompile 明确不带工程 id，这条守的是「一个区块配不出工程，
// 整页发布不了」。
func TestOrderListWithoutProjectStaysCompilable(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := orderListDoc(t, map[string]any{})
	compiled, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("缺工程 id 不该让整页编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	if !strings.Contains(doc, "暂不可用") {
		t.Errorf("缺工程 id 时应渲染可见提示；实际产物：%s", doc)
	}
	if strings.Contains(doc, "/_fragments/ordersList") {
		t.Errorf("缺工程 id 时不该输出片段地址（拉不到，只会得到一个错误）")
	}
}

// TestOrderListInjectsHtmx 产物里带 hx-* 属性时必须同时带上 htmx 运行时。
//
// 这是「访问面没有 htmx」那个缺口的回归保护：hx-get 写了但没运行时，
// 表现是容器永远停在默认内容（看起来只是「没数据」）。
func TestOrderListInjectsHtmx(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := orderListDoc(t, map[string]any{})
	compiled, err := Compile(page,
		WithComponentSet(set),
		WithProjectID("proj-9"),
		WithUISources(map[string]string{
			"htmx.min.js": "/* htmx-runtime */ var htmx = {};",
			"_util.js":    "/* util */",
			"select.js":   "/* select */",
			"modal.js":    "/* modal */",
			"index.js":    "/* index */",
		}),
		WithUIStyle("/* ui css */"),
	)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	if !strings.Contains(doc, "/* htmx-runtime */") {
		t.Errorf("产物带 hx-* 属性却没有 htmx 运行时 —— 容器会永远停在默认内容；实际产物：%s", doc)
	}
}
