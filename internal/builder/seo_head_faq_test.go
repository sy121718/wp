package builder

import (
	"strings"
	"testing"
)

// seo_head_faq_test.go — FAQ 结构化数据的 head 侧行为（审计 SEO-006）。
//
// FAQPage 的数据来自 core.faq 组件（同源问答，见 components/faq/jsonld.go）。
// head 侧只需要做一件事：不要在 schemaType=faq 时再输出一个没有 mainEntity 的空壳 ——
// 两个同类型节点里的那个空壳，在 Search Console 里是「无效结构化数据」。

// TestSEOHeadFAQSchemaTypeHasNoEmptyShell schemaType=faq 时不输出 FAQPage 空壳。
func TestSEOHeadFAQSchemaTypeHasNoEmptyShell(t *testing.T) {
	t.Setenv(siteEnvKey, "https://shop.test")

	head := BuildSEOHead(SEO{Title: "常见问题", SchemaType: "faq"}, "/faq", "", "", "首页", nil)
	if strings.Contains(head, "FAQPage") {
		t.Fatalf("head 不应输出 FAQPage（那份由 core.faq 组件输出）：%s", head)
	}
	if !strings.Contains(head, `"@type":"WebPage"`) {
		t.Errorf("schemaType=faq 的页面应按默认 WebPage 输出主实体：%s", head)
	}
}

// TestSEOHeadFAQSchemaTypeNotHomeStillPageLevel schemaType=faq 的首页也不因此产出站点级 FAQ 断言。
func TestSEOHeadFAQSchemaTypeNotHomeStillPageLevel(t *testing.T) {
	t.Setenv(siteEnvKey, "https://shop.test")

	head := BuildSEOHead(SEO{Title: "常见问题", SchemaType: "faq", OGImage: "https://shop.test/l.png"}, "/", "", "", "首页", nil)
	nodes := headJSONLDNodes(t, head)
	if n := findJSONLDNode(nodes, "FAQPage"); n != nil {
		t.Errorf("head 不应有 FAQPage 节点：%#v", n)
	}
	if findJSONLDNode(nodes, "Organization") == nil || findJSONLDNode(nodes, "WebSite") == nil {
		t.Errorf("站点级节点仍应输出：%s", head)
	}
}
