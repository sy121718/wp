package builder

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"
)

const (
	jsonLDOpen  = `<script type="application/ld+json">`
	jsonLDClose = "/script"
	siteEnvKey  = "WP_SITE_BASE_URL"
)

func headJSONLD(t *testing.T, head string) map[string]any {
	t.Helper()
	start := strings.Index(head, jsonLDOpen)
	if start < 0 {
		t.Fatalf("SEO 头里没有 JSON-LD；输出：%s", head)
	}
	start += len(jsonLDOpen)
	end := strings.Index(head[start:], "<"+jsonLDClose)
	if end < 0 {
		t.Fatalf("JSON-LD 未闭合；输出：%s", head)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(head[start:start+end]), &doc); err != nil {
		t.Fatalf("JSON-LD 不是合法 JSON：%v；片段：%s", err, head[start:start+end])
	}
	return doc
}

func headJSONLDNodes(t *testing.T, head string) []map[string]any {
	t.Helper()
	doc := headJSONLD(t, head)
	graph, ok := doc["@graph"].([]any)
	if !ok {
		return []map[string]any{doc}
	}
	nodes := make([]map[string]any, 0, len(graph))
	for _, item := range graph {
		node, isObj := item.(map[string]any)
		if !isObj {
			t.Fatalf("@graph 里出现非对象节点：%#v", item)
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func findJSONLDNode(nodes []map[string]any, typeName string) map[string]any {
	for _, n := range nodes {
		if t, _ := n["@type"].(string); t == typeName {
			return n
		}
	}
	return nil
}

func countJSONLDNodes(nodes []map[string]any, typeName string) int {
	n := 0
	for _, node := range nodes {
		if t, _ := node["@type"].(string); t == typeName {
			n++
		}
	}
	return n
}

func searchActionOf(t *testing.T, site map[string]any) map[string]any {
	t.Helper()
	action, ok := site["potentialAction"].(map[string]any)
	if !ok {
		t.Fatalf("WebSite 缺少 potentialAction：%#v", site)
	}
	if got, _ := action["@type"].(string); got != "SearchAction" {
		t.Fatalf("potentialAction 类型应为 SearchAction，实际 %v", action["@type"])
	}
	return action
}

func TestSiteLevelJSONLDOnHomePage(t *testing.T) {
	t.Setenv(siteEnvKey, "https://shop.test/site")

	head := BuildSEOHead(SEO{
		Title:   "示例站点",
		OGImage: "https://shop.test/logo.png",
	}, "/", "", "", "首页", nil)

	if got := strings.Count(head, "application/ld+json"); got != 1 {
		t.Fatalf("站点级节点应并入同一份 JSON-LD，实际 %d 份：%s", got, head)
	}
	nodes := headJSONLDNodes(t, head)

	org := findJSONLDNode(nodes, "Organization")
	if org == nil {
		t.Fatalf("首页缺少 Organization 节点：%s", head)
	}
	if org["name"] != "示例站点" {
		t.Errorf("Organization.name = %v，期望 示例站点", org["name"])
	}

	if org["url"] != "https://shop.test/site/" {
		t.Errorf("Organization.url = %v，期望 https://shop.test/site/", org["url"])
	}
	if org["logo"] != "https://shop.test/logo.png" {
		t.Errorf("Organization.logo = %v，期望 OG 图", org["logo"])
	}

	site := findJSONLDNode(nodes, "WebSite")
	if site == nil {
		t.Fatalf("首页缺少 WebSite 节点：%s", head)
	}
	if site["url"] != "https://shop.test/site/" {
		t.Errorf("WebSite.url = %v，期望 https://shop.test/site/", site["url"])
	}
	action := searchActionOf(t, site)
	target, ok := action["target"].(map[string]any)
	if !ok {
		t.Fatalf("SearchAction.target 应为对象：%#v", action["target"])
	}
	if target["@type"] != "EntryPoint" {
		t.Errorf("target.@type = %v，期望 EntryPoint", target["@type"])
	}

	if target["urlTemplate"] != "https://shop.test/site/?q={search_term_string}" {
		t.Errorf("urlTemplate = %v", target["urlTemplate"])
	}
	if action["query-input"] != "required name=search_term_string" {
		t.Errorf("query-input = %v", action["query-input"])
	}
	if findJSONLDNode(nodes, "WebPage") == nil {
		t.Errorf("页面级主实体不应被站点级节点挤掉：%s", head)
	}
}

func TestSiteLevelJSONLDNotOnInnerPages(t *testing.T) {
	t.Setenv(siteEnvKey, "https://shop.test")

	head := BuildSEOHead(SEO{Title: "商品页"}, "/products/a", "", "", "首页", nil)
	nodes := headJSONLDNodes(t, head)
	for _, banned := range []string{"Organization", "WebSite"} {
		if n := findJSONLDNode(nodes, banned); n != nil {
			t.Errorf("非首页不应输出 %s 节点：%#v", banned, n)
		}
	}
	if _, hasGraph := headJSONLD(t, head)["@graph"]; hasGraph {
		t.Errorf("非首页不应退化成 @graph 结构：%s", head)
	}
}

func TestSiteLevelJSONLDWebsiteSchemaKeepsSingleWebsite(t *testing.T) {
	t.Setenv(siteEnvKey, "https://shop.test")

	head := BuildSEOHead(SEO{Title: "示例站点", SchemaType: "website"}, "/", "", "", "首页", nil)
	nodes := headJSONLDNodes(t, head)
	if got := countJSONLDNodes(nodes, "WebSite"); got != 1 {
		t.Fatalf("WebSite 节点应恰好 1 个，实际 %d：%s", got, head)
	}
	site := findJSONLDNode(nodes, "WebSite")
	action := searchActionOf(t, site)
	if target, _ := action["target"].(map[string]any); target["urlTemplate"] != "https://shop.test/?q={search_term_string}" {
		t.Errorf("SearchAction 应合并进页面主实体，target=%#v", action["target"])
	}
	if findJSONLDNode(nodes, "Organization") == nil {
		t.Errorf("Organization 仍应输出：%s", head)
	}
}

func TestSiteLevelJSONLDSkippedWithoutBaseURL(t *testing.T) {
	t.Setenv(siteEnvKey, "")

	head := BuildSEOHead(SEO{Title: "示例站点"}, "/", "", "", "首页", nil)
	nodes := headJSONLDNodes(t, head)
	if n := findJSONLDNode(nodes, "Organization"); n != nil {
		t.Errorf("站点根未知时不应输出 Organization：%#v", n)
	}
	if n := findJSONLDNode(nodes, "WebSite"); n != nil {
		t.Errorf("站点根未知时不应输出 WebSite：%#v", n)
	}
	if findJSONLDNode(nodes, "WebPage") == nil {
		t.Errorf("页面级主实体应保持输出：%s", head)
	}
}

func TestSiteLevelJSONLDFallsBackToAbsoluteCanonical(t *testing.T) {
	t.Setenv(siteEnvKey, "")

	head := BuildSEOHead(SEO{Title: "示例站点"}, "https://shop.test/", "", "", "首页", nil)
	site := findJSONLDNode(headJSONLDNodes(t, head), "WebSite")
	if site == nil {
		t.Fatalf("绝对 canonical 应能推出站点根：%s", head)
	}
	if site["url"] != "https://shop.test/" {
		t.Errorf("WebSite.url = %v，期望 https://shop.test/", site["url"])
	}
}

func TestSiteLevelJSONLDWithoutTitleSkipsOrganization(t *testing.T) {
	t.Setenv(siteEnvKey, "https://shop.test")

	head := BuildSEOHead(SEO{Description: "只有描述"}, "/", "", "", "首页", nil)
	if n := findJSONLDNode(headJSONLDNodes(t, head), "Organization"); n != nil {
		t.Errorf("无站点名不应输出 Organization：%#v", n)
	}
}
