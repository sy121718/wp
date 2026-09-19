package productcard

// i18n_test.go — 商品卡链接前缀的站内本地化（审计 I18N-015）。
//
// 只本地化**作者填的前缀**（/products/），CMS 字段值原样参与拼接：
// 字段值可能是纯 slug（"shirt"）、完整路径（"/products/shirt"）或外链，
// 后两种由 CardHref 原样返回，再前缀一次就会指到不存在的地址 ——
// 与 button 的 ActionLink「CMS 绑定值不本地化」同一条口径。

import "testing"

// siteLinkOf 模拟装配层注入的本地化器：只对站内路径加前缀。
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

// TestBuildViewLocalizesLinkPrefix 作者填的站内前缀按语言加前缀。
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

// TestBuildViewWithoutSiteLinkKeepsHref 未注入本地化器时链接与接入前逐字一致。
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

// TestBuildViewAbsoluteCMSValueUntouched 字段值本身是完整路径（CMS 给的）→ 不二次前缀。
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
