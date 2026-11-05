package builder

import (
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/templates"
	breadcrumbPkg "go_wp/pkg/builder/components/breadcrumb"
	"go_wp/pkg/builder/core"
)

const breadcrumbDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "base": {}, "seo": {"title": "面包屑", "canonical": "/products/pods"}},
  "root": [
    {"id": "bc1", "type": "core.breadcrumb", "props": {}}
  ]
}`

var crumbTextRe = regexp.MustCompile(`>([^<>]+)</(?:a|span)>`)

var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

type crumbLD struct {
	Name string `json:"name"`
	Item string `json:"item"`
}

func compileBreadcrumbDoc(t *testing.T, opts ...CompileOption) *CompiledPage {
	t.Helper()
	page, err := ParsePage([]byte(breadcrumbDocJSON))
	if err != nil {
		t.Fatalf("页面文档解析失败: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set 加载失败: %v", err)
	}
	compiled, err := Compile(page, append([]CompileOption{WithComponentSet(set)}, opts...)...)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return compiled
}

func crumbListSegment(t *testing.T, html string) string {
	t.Helper()
	const marker = `class="sky-breadcrumb-list"`
	start := strings.Index(html, marker)
	if start < 0 {
		t.Fatalf("产物没有可见面包屑列表\nHTML=%s", html)
	}
	rest := html[start:]
	end := strings.Index(rest, "</ol>")
	if end < 0 {
		t.Fatalf("面包屑列表未闭合\nHTML=%s", html)
	}
	return rest[:end]
}

func visibleCrumbs(t *testing.T, html string) []string {
	t.Helper()
	var out []string
	for _, m := range crumbTextRe.FindAllStringSubmatch(crumbListSegment(t, html), -1) {
		out = append(out, m[1])
	}
	return out
}

func visibleCrumbHrefs(t *testing.T, html string) []string {
	t.Helper()
	var out []string
	for _, m := range hrefRe.FindAllStringSubmatch(crumbListSegment(t, html), -1) {
		out = append(out, m[1])
	}
	return out
}

func headBreadcrumbList(t *testing.T, head string) []crumbLD {
	t.Helper()
	const open = `<script type="application/ld+json">`
	i := strings.Index(head, open)
	if i < 0 {
		t.Fatalf("SEO 头没有 JSON-LD 脚本\n%s", head)
	}
	rest := head[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatalf("JSON-LD 脚本未闭合\n%s", head)
	}
	var doc struct {
		Breadcrumb struct {
			Type  string    `json:"@type"`
			Items []crumbLD `json:"itemListElement"`
		} `json:"breadcrumb"`
	}
	if err := json.Unmarshal([]byte(rest[:j]), &doc); err != nil {
		t.Fatalf("JSON-LD 解析失败: %v\n%s", err, rest[:j])
	}
	if doc.Breadcrumb.Type != "BreadcrumbList" {
		t.Fatalf("JSON-LD 面包屑类型不符: %q", doc.Breadcrumb.Type)
	}
	return doc.Breadcrumb.Items
}

func headBreadcrumbNames(t *testing.T, head string) []string {
	t.Helper()
	items := headBreadcrumbList(t, head)
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.Name)
	}
	return names
}

func headBreadcrumbItems(t *testing.T, head string) []string {
	t.Helper()
	items := headBreadcrumbList(t, head)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Item)
	}
	return out
}

func TestBreadcrumbVisibleDomMatchesHeadJSONLD(t *testing.T) {
	compiled := compileBreadcrumbDoc(t, WithCurrentPath("/products/pods"),
		WithTranslator(func(key, fallback string) string {
			if key == breadcrumbPkg.TextKeyHome {
				return "CRUMB-HOME"
			}
			return fallback
		}))

	got := visibleCrumbs(t, compiled.HTML)
	want := []string{"CRUMB-HOME", "products", "pods"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("可见面包屑层级不符：got=%v want=%v", got, want)
	}

	for _, needle := range []string{
		`href="/products"`,
		`aria-current="page"`,
		`href="/products/pods"`,
	} {
		if !strings.Contains(compiled.HTML, needle) {
			t.Errorf("产物缺少 %q", needle)
		}
	}

	if n := strings.Count(compiled.HTML, `aria-current="page"`); n != 1 {
		t.Errorf("aria-current 只应出现在当前项，实际出现 %d 次", n)
	}
	if !strings.Contains(compiled.HTML, `aria-hidden="true"`) {
		t.Errorf("分隔符应标 aria-hidden（否则读屏会念出分隔符）")
	}

	head := BuildSEOHead(SEO{Title: "面包屑"}, "/products/pods", "", "", "CRUMB-HOME", nil)
	headNames := headBreadcrumbNames(t, head)
	if strings.Join(headNames, "|") != strings.Join(got, "|") {
		t.Fatalf("可见项与 JSON-LD 不同源：head=%v visible=%v", headNames, got)
	}

	if fileNames := headBreadcrumbNames(t, compiled.SEOHead); strings.Join(fileNames, "|") != strings.Join(got, "|") {
		t.Fatalf("产物 head 的 JSON-LD 与可见项不同源：head=%v visible=%v", fileNames, got)
	}

	if hrefs := visibleCrumbHrefs(t, compiled.HTML); strings.Join(hrefs, "|") != strings.Join(headBreadcrumbItems(t, compiled.SEOHead), "|") {
		t.Fatalf("可见项链接与 JSON-LD item 不同源：ld=%v visible=%v", headBreadcrumbItems(t, compiled.SEOHead), hrefs)
	}
}

func TestBreadcrumbI18nUsesKeys(t *testing.T) {
	asked := map[string]bool{}
	compiled := compileBreadcrumbDoc(t, WithCurrentPath("/about"),
		WithTranslator(func(key, fallback string) string {
			asked[key] = true
			switch key {
			case breadcrumbPkg.TextKeyLabel:
				return "Breadcrumb"
			case breadcrumbPkg.TextKeyHome:
				return "Home"
			}
			return fallback
		}))
	if !asked[breadcrumbPkg.TextKeyLabel] || !asked[breadcrumbPkg.TextKeyHome] {
		t.Fatalf("构建期未按词条 key 取词，实际请求：%v", asked)
	}
	if !strings.Contains(compiled.HTML, `aria-label="Breadcrumb"`) {
		t.Fatalf("容器标签未使用词条\nHTML=%s", compiled.HTML)
	}
	if got := visibleCrumbs(t, compiled.HTML); strings.Join(got, "|") != "Home|about" {
		t.Fatalf("首页项未使用词条：%v", got)
	}
	if strings.Contains(compiled.HTML, "面包屑导航") {
		t.Errorf("命中词条后仍出现中文兜底\nHTML=%s", compiled.HTML)
	}

	plain := compileBreadcrumbDoc(t, WithCurrentPath("/about"))
	if !strings.Contains(plain.HTML, `aria-label="面包屑导航"`) {
		t.Fatalf("缺词条时应回退中文兜底\nHTML=%s", plain.HTML)
	}
	if strings.Contains(plain.HTML, `aria-label=""`) {
		t.Fatalf("产物出现空 aria-label\nHTML=%s", plain.HTML)
	}
}

func TestBreadcrumbJSONLDOption(t *testing.T) {
	doc := strings.Replace(breadcrumbDocJSON, `"props": {}`, `"props": {"jsonLd": true}`, 1)
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("页面文档解析失败: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set 加载失败: %v", err)
	}
	compiled, err := Compile(page, WithComponentSet(set), WithCurrentPath("/products/pods"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	visible := visibleCrumbs(t, compiled.HTML)
	if len(visible) != 3 {
		t.Fatalf("可见项数不符：%v", visible)
	}

	const openTag = `<script type="application/ld+json">`
	idx := strings.LastIndex(compiled.HTML, `"@type":"BreadcrumbList"`)
	if idx < 0 {
		t.Fatalf("开启 jsonLd 后产物仍无 BreadcrumbList\nHTML=%s", compiled.HTML)
	}
	open := strings.LastIndex(compiled.HTML[:idx], openTag)
	if open < 0 {
		t.Fatalf("未找到组件自带脚本起始标签")
	}
	body := compiled.HTML[open+len(openTag):]
	if end := strings.Index(body, "</script>"); end >= 0 {
		body = body[:end]
	}
	var doc2 struct {
		Type  string `json:"@type"`
		Items []struct {
			Name string `json:"name"`
			Item string `json:"item"`
		} `json:"itemListElement"`
	}
	if err := json.Unmarshal([]byte(body), &doc2); err != nil {
		t.Fatalf("组件自带 JSON-LD 解析失败: %v\n%s", err, body)
	}
	if doc2.Type != "BreadcrumbList" {
		t.Fatalf("组件自带 JSON-LD 类型不符: %q", doc2.Type)
	}
	names := make([]string, 0, len(doc2.Items))
	for _, it := range doc2.Items {
		names = append(names, it.Name)
	}
	if strings.Join(names, "|") != strings.Join(visible, "|") {
		t.Fatalf("组件自带 JSON-LD 与可见项不同源：ld=%v visible=%v", names, visible)
	}
}

func TestBreadcrumbRegistered(t *testing.T) {
	comp, err := core.Lookup(breadcrumbPkg.Type)
	if err != nil || comp == nil {
		t.Fatalf("组件 %s 未注册: %v", breadcrumbPkg.Type, err)
	}
	if _, ok := core.OwnedTemplate("breadcrumb"); !ok {
		t.Fatalf("模板 breadcrumb 未注册（页面渲染会报 template not found）")
	}
	found := false
	for _, name := range core.Types() {
		if name == breadcrumbPkg.Type {
			found = true
		}
	}
	if !found {
		t.Fatalf("core.Types() 不含 %s（组件库看不到它）", breadcrumbPkg.Type)
	}
}
