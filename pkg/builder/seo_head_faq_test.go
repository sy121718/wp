package builder

import (
	"strings"
	"testing"
)

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
