package productcard

import "testing"

func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

func productCardProps(t *testing.T, prefix string) Props {
	t.Helper()
	return decodePropsOf(t, map[string]any{
		"titleField": "item.name", "linkField": "item.slug", "linkPrefix": prefix,
	})
}

func TestBuildViewLocalizesLinkPrefix(t *testing.T) {
	p := productCardProps(t, "/products/")
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"item.name": "衬衫", "item.slug": "shirt",
	}}, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Href != "/en/products/shirt" {
		t.Fatalf("详情地址应带语言前缀，实际 %q", view.Href)
	}
}

func TestBuildViewWithoutSiteLinkKeepsHref(t *testing.T) {
	p := productCardProps(t, "/products/")
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"item.name": "衬衫", "item.slug": "shirt",
	}}, nil)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Href != "/products/shirt" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", view.Href)
	}
}

func TestBuildViewAbsoluteCMSValueUntouched(t *testing.T) {
	p := productCardProps(t, "/products/")
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"item.name": "衬衫", "item.slug": "/products/shirt",
	}}, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Href != "/products/shirt" {
		t.Fatalf("CMS 给的完整路径不该二次前缀，实际 %q", view.Href)
	}
}
