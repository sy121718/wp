package templates

import (
	"strings"
	"testing"
)

func TestProductStatusLabelRendersKnownAndUnknownStates(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   string
	}{
		{name: "草稿", status: "draft", want: "草稿"},
		{name: "已上架", status: "published", want: "已上架"},
		{name: "已下架", status: "archived", want: "已下架"},
		{name: "未知状态原样显示", status: "on_hold", want: "on_hold"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := render(t, groupDSet(t), "admin/product/status_label.html",
				groupDData(map[string]any{"status": tc.status}))
			if err != nil {
				t.Fatalf("渲染状态片段失败: %v", err)
			}
			if strings.Contains(out, "{{") {
				t.Fatalf("状态片段输出残留模板语法字面量: %s", out)
			}
			if !strings.Contains(out, ">"+tc.want+"<") {
				t.Fatalf("状态 %q 应渲染为 %q，实际 %s", tc.status, tc.want, out)
			}
			if !strings.Contains(out, "badge badge-") {
				t.Fatalf("状态片段应输出徽标形态，实际 %s", out)
			}
			if tc.status == "on_hold" && strings.Contains(out, "全部状态") {
				t.Fatalf("未知状态不该回落到筛选文案: %s", out)
			}
		})
	}
}

func TestProductStatusRendersThroughOnePartial(t *testing.T) {
	for _, tpl := range []string{"product/products.html", "product/product_detail.html"} {
		out := renderAdminPage(t, tpl, productCatalogData(nil))
		if !strings.Contains(out, "badge badge-warning") || !strings.Contains(out, "草稿") {
			t.Fatalf("%s 的草稿状态应经共享片段渲染成「草稿」徽标，实际未命中", tpl)
		}
		if strings.Contains(out, `<span class="badge">draft</span>`) {
			t.Fatalf("%s 仍把原始状态值直接塞进徽标（说明没走共享片段）", tpl)
		}
	}
}

// 详情页的「商品不存在」分支已经删掉：取不到商品时控制器直接交 JSON 失败，
// 成功页永远有 Product，模板里不该再留 isset(.Product) 的空态分支。
func TestProductDetailTemplateHasNoNotFoundBranch(t *testing.T) {
	text := adminTemplateSource(t, "product_detail.html")
	if strings.Contains(text, "admin.product_detail.notFound") {
		t.Fatalf("详情模板仍保留不可达的「商品不存在」空态分支")
	}
	if strings.Contains(text, "isset(.Product)") {
		t.Fatalf("详情模板仍在按 Product 是否存在分叉：成功页必然带 Product，失败走 JSON")
	}
}

func TestProductStatusLabelUsesInjectedTranslator(t *testing.T) {
	seen := make([]string, 0, 4)
	tr := func(key, fallback string) string {
		seen = append(seen, key)
		return "EN:" + fallback
	}
	for _, status := range []string{"draft", "published", "archived"} {
		seen = seen[:0]
		out, err := render(t, groupDSet(t), "admin/product/status_label.html",
			groupDData(map[string]any{"status": status, "t": tr}))
		if err != nil {
			t.Fatalf("渲染状态片段失败: %v", err)
		}
		wantKey := map[string]string{"draft": "pStatusDraft", "published": "pStatusLive", "archived": "pStatusArchived"}[status]
		if len(seen) != 1 || seen[0] != wantKey {
			t.Fatalf("状态 %q 应恰好取词一次 %q，实际 %v", status, wantKey, seen)
		}
		if !strings.Contains(out, "EN:") {
			t.Fatalf("状态 %q 的文案应来自注入的取词函数，实际 %s", status, out)
		}
	}
	seen = seen[:0]
	if _, err := render(t, groupDSet(t), "admin/product/status_label.html",
		groupDData(map[string]any{"status": "on_hold", "t": tr})); err != nil {
		t.Fatalf("渲染未知状态失败: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("未知状态不该取任何界面词条，实际 %v", seen)
	}
}
