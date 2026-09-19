package gallery

// i18n_test.go — 图集站内链接的本地化（审计 I18N-015）。
//
// 三条口径：
//  1. **作者手填**的站内路径（集合项 link / 兜底 defaultLink）按当前语言加前缀；
//  2. 未注入本地化器时原样输出 —— 缺前缀是可见降级，抛错会让整页构建失败；
//  3. CMS 绑定值里的 link **不**本地化：那个地址的语义由内容作者掌握（可能已经是
//     完整访问路径），再前缀一次会指到不存在的页面（与 button 的 ActionLink 同一口径）。
//
// 判据只能用**数据来源**（p.Items 还是 Binding），不能用字符串形状 —— 两者都是 "/xxx"。

import "testing"

// siteLinkOf 模拟装配层注入的真实本地化器：只对站内路径加前缀，外链/协议相对地址原样放行。
func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

// galleryResolver 固定字段值的 CMS 解析器。
type galleryResolver struct{ value string }

func (r galleryResolver) ResolveString(string) (string, error) { return r.value, nil }

// TestBuildViewLocalizesAuthorItemLink 作者手填的集合项 link 按语言加前缀。
func TestBuildViewLocalizesAuthorItemLink(t *testing.T) {
	p := Props{ClickAction: ClickLink, Items: []Item{{URL: "/storage/a.jpg", Link: "/about"}}}
	view, err := BuildView("g1", &p, nil, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Items) != 1 || !view.Items[0].IsLink || view.Items[0].Href != "/en/about" {
		t.Fatalf("作者手填的站内链接应加语言前缀，实际 %+v", view.Items)
	}
}

// TestBuildViewLocalizesDefaultLink 兜底链接（作者在组件属性上填的）同样本地化。
func TestBuildViewLocalizesDefaultLink(t *testing.T) {
	p := Props{ClickAction: ClickLink, DefaultLink: "/about", Items: []Item{{URL: "/storage/a.jpg"}}}
	view, err := BuildView("g1", &p, nil, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.Items[0].IsLink || view.Items[0].Href != "/en/about" {
		t.Fatalf("兜底链接应加语言前缀，实际 %+v", view.Items[0])
	}
}

// TestBuildViewKeepsBindingLinkUntouched CMS 绑定值里的 link 不本地化。
func TestBuildViewKeepsBindingLinkUntouched(t *testing.T) {
	p := Props{ClickAction: ClickLink, Binding: &Binding{Field: "post.gallery"}}
	resolver := galleryResolver{value: "[{\"url\":\"/storage/a.jpg\",\"link\":\"/about\"}]"}
	view, err := BuildView("g1", &p, resolver, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Items) != 1 || view.Items[0].Href != "/about" {
		t.Fatalf("CMS 绑定值不应被二次前缀，实际 %+v", view.Items)
	}
}

// TestBuildViewWithoutSiteLink 未注入本地化器时原样输出（单测 / 独立编译路径）。
func TestBuildViewWithoutSiteLink(t *testing.T) {
	p := Props{ClickAction: ClickLink, Items: []Item{{URL: "/storage/a.jpg", Link: "/about"}}}
	view, err := BuildView("g1", &p, nil, nil)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Items[0].Href != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", view.Items[0].Href)
	}
}

// TestBuildViewExternalLinkUntouched 外链不因本地化被改写。
func TestBuildViewExternalLinkUntouched(t *testing.T) {
	p := Props{ClickAction: ClickLink, Items: []Item{{URL: "/storage/a.jpg", Link: "https://example.com/x"}}}
	view, err := BuildView("g1", &p, nil, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Items[0].Href != "https://example.com/x" {
		t.Fatalf("外链不该被改写，实际 %q", view.Items[0].Href)
	}
}
