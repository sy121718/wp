package breadcrumb

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

type ldDocView struct {
	Context         string       `json:"@context"`
	Type            string       `json:"@type"`
	ItemListElement []ldItemView `json:"itemListElement"`
}

type ldItemView struct {
	Type     string `json:"@type"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Item     string `json:"item,omitempty"`
}

func scriptJSONLD(t *testing.T, v View) string {
	t.Helper()
	const open = `<script type="application/ld+json">`
	if !strings.HasPrefix(v.JSONLD, open) || !strings.HasSuffix(v.JSONLD, "</script>") {
		t.Fatalf("JSON-LD 外壳不符: %q", v.JSONLD)
	}
	return strings.TrimSuffix(strings.TrimPrefix(v.JSONLD, open), "</script>")
}

func TestDeriveItemsFollowsPath(t *testing.T) {
	want := []Item{
		{Label: "首页", URL: "/"},
		{Label: "products", URL: "/products"},
		{Label: "pods", URL: "/products/pods"},
	}
	got := deriveItems("/products/pods", "首页")
	if len(got) != len(want) {
		t.Fatalf("层级项数不符：got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项不符：got=%+v want=%+v", i+1, got[i], want[i])
		}
	}

	if trail := deriveItems("/about/", "首页"); len(trail) != 2 || trail[1].URL != "/about" {
		t.Errorf("尾斜杠应被忽略，实际 %+v", trail)
	}

	for _, p := range []string{"", "   "} {
		if got := deriveItems(p, "首页"); got != nil {
			t.Errorf("路径 %q 应返回 nil，实际 %+v", p, got)
		}
	}
}

func TestBuildViewDerivesFromCurrentPath(t *testing.T) {
	v := BuildView(&Props{}, &core.RenderContext{CurrentPath: "/products/pods"})
	if !v.Visible || len(v.Items) != 3 {
		t.Fatalf("应按路径派生 3 项，实际 visible=%v items=%+v", v.Visible, v.Items)
	}
	if v.Items[0].Separator != "/" || v.Items[1].Separator != "/" {
		t.Errorf("非末项应带缺省分隔符 /，实际 %+v", v.Items)
	}
	if v.Items[2].Separator != "" {
		t.Errorf("末项不应带分隔符，实际 %+v", v.Items[2])
	}
	if !v.Items[2].Current || v.Items[0].Current {
		t.Errorf("只有末项应标记为当前页，实际 %+v", v.Items)
	}

	v2 := BuildView(&Props{Separator: ">"}, &core.RenderContext{CurrentPath: "/a/b"})
	if v2.Items[0].Separator != ">" {
		t.Errorf("自定义分隔符未生效，实际 %+v", v2.Items[0])
	}
}

func TestBuildViewHideHomeAndNoPath(t *testing.T) {
	if v := BuildView(&Props{}, &core.RenderContext{CurrentPath: "/"}); v.Visible {
		t.Errorf("首页自身不应渲染面包屑，实际 %+v", v.Items)
	}
	if v := BuildView(&Props{}, nil); v.Visible {
		t.Errorf("无构建上下文时不应渲染面包屑（不猜层级），实际 %+v", v.Items)
	}

	hidden := BuildView(&Props{HideHome: true}, &core.RenderContext{CurrentPath: "/a/b"})
	if len(hidden.Items) != 2 || hidden.Items[0].URL != "/a" {
		t.Fatalf("hideHome 应去掉首页项，实际 %+v", hidden.Items)
	}

	manual := BuildView(&Props{Items: []Item{
		{Label: "首页", URL: "/"},
		{Label: "博客", URL: "/blog"},
	}}, &core.RenderContext{CurrentPath: "/products/pods"})
	if len(manual.Items) != 2 || manual.Items[1].Label != "博客" || manual.Items[1].URL != "/blog" {
		t.Fatalf("手填层级应优先于路径派生，实际 %+v", manual.Items)
	}
}

func TestApplyI18nFillsLabelAndHome(t *testing.T) {
	translate := func(key, fallback string) string {
		switch key {
		case TextKeyLabel:
			return "Breadcrumb"
		case TextKeyHome:
			return "Home"
		}
		return fallback
	}

	v := BuildView(&Props{}, &core.RenderContext{CurrentPath: "/about"})
	v.ApplyI18n(translate)
	if v.Label != "Breadcrumb" || v.Items[0].Label != "Home" {
		t.Errorf("应取到词条，实际 label=%q 首项=%q", v.Label, v.Items[0].Label)
	}

	v2 := BuildView(&Props{}, &core.RenderContext{CurrentPath: "/about"})
	v2.ApplyI18n(nil)
	if v2.Label != "面包屑导航" || v2.Items[0].Label != "首页" {
		t.Errorf("无取词函数应回退中文兜底，实际 label=%q 首项=%q", v2.Label, v2.Items[0].Label)
	}

	v3 := BuildView(&Props{HomeLabel: "主页"}, &core.RenderContext{CurrentPath: "/about"})
	v3.ApplyI18n(translate)
	if v3.Items[0].Label != "主页" {
		t.Errorf("作者填写的首页名不应被词条覆盖，实际 %q", v3.Items[0].Label)
	}
}

func TestJSONLDMatchesVisibleItems(t *testing.T) {
	v := BuildView(&Props{JSONLD: true}, &core.RenderContext{CurrentPath: "/products/pods"})
	v.ApplyI18n(nil)

	var doc ldDocView
	if err := json.Unmarshal([]byte(scriptJSONLD(t, v)), &doc); err != nil {
		t.Fatalf("JSON-LD 解析失败: %v", err)
	}
	if doc.Type != "BreadcrumbList" || len(doc.ItemListElement) != len(v.Items) {
		t.Fatalf("脚本与可见项数量不符: type=%q items=%d visible=%d", doc.Type, len(doc.ItemListElement), len(v.Items))
	}
	for i, it := range doc.ItemListElement {
		if it.Position != i+1 || it.Name != v.Items[i].Label || it.Item != v.Items[i].URL {
			t.Errorf("第 %d 项与可见项不一致: 脚本=%+v 可见=%+v", i+1, it, v.Items[i])
		}
	}

	v2 := BuildView(&Props{}, &core.RenderContext{CurrentPath: "/products/pods"})
	v2.ApplyI18n(nil)
	if v2.JSONLD != "" {
		t.Errorf("默认不应输出 JSON-LD，实际 %q", v2.JSONLD)
	}

	v3 := BuildView(&Props{JSONLD: true, Items: []Item{{Label: "</script><script>alert(1)</script>", URL: "/x"}}}, nil)
	v3.ApplyI18n(nil)
	if got := strings.Count(v3.JSONLD, "</script>"); got != 1 {
		t.Errorf("脚本标签数不符（可能被注入闭合）: %d\n%s", got, v3.JSONLD)
	}
	if !strings.Contains(v3.JSONLD, `\u003c/script`) {
		t.Errorf("尖括号应被 JSON 序列化转义，实际 %s", v3.JSONLD)
	}
}

func TestValidateExtraRejectsBadItems(t *testing.T) {
	bad := []struct {
		name  string
		props Props
		want  string
	}{
		{"空文案", Props{Items: []Item{{Label: "  ", URL: "/a"}}}, "缺少文字"},
		{"非法链接", Props{Items: []Item{{Label: "关于", URL: "javascript:alert(1)"}}}, "链接非法"},
		{"隐藏首页却无层级", Props{HideHome: true}, "必须手工提供"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExtra(&tc.props, "n1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应报 %q，实际 %v", tc.want, err)
			}
		})
	}

	ok := Props{Items: []Item{{Label: "关于", URL: "/about"}}, Separator: ">"}
	if err := validateExtra(&ok, "n1"); err != nil {
		t.Fatalf("合法属性不应报错: %v", err)
	}
}
