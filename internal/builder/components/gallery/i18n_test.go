package gallery

import "testing"

func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

type galleryResolver struct{ value string }

func (r galleryResolver) ResolveString(string) (string, error) { return r.value, nil }

func TestBuildViewLocalizesAuthorItemLink(t *testing.T) {
	p := Props{ClickAction: ClickLink, Items: []Item{{URL: "https://media.example.test/storage/a.jpg", Link: "/about"}}}
	view, err := BuildView("g1", &p, nil, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Items) != 1 || !view.Items[0].IsLink || view.Items[0].Href != "/en/about" {
		t.Fatalf("作者手填的站内链接应加语言前缀，实际 %+v", view.Items)
	}
}

func TestBuildViewLocalizesDefaultLink(t *testing.T) {
	p := Props{ClickAction: ClickLink, DefaultLink: "/about", Items: []Item{{URL: "https://media.example.test/storage/a.jpg"}}}
	view, err := BuildView("g1", &p, nil, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !view.Items[0].IsLink || view.Items[0].Href != "/en/about" {
		t.Fatalf("兜底链接应加语言前缀，实际 %+v", view.Items[0])
	}
}

func TestBuildViewKeepsBindingLinkUntouched(t *testing.T) {
	p := Props{ClickAction: ClickLink, Binding: &Binding{Field: "post.gallery"}}
	resolver := galleryResolver{value: "[{\"url\":\"https://media.example.test/storage/a.jpg\",\"link\":\"/about\"}]"}
	view, err := BuildView("g1", &p, resolver, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Items) != 1 || view.Items[0].Href != "/about" {
		t.Fatalf("CMS 绑定值不应被二次前缀，实际 %+v", view.Items)
	}
}

func TestBuildViewWithoutSiteLink(t *testing.T) {
	p := Props{ClickAction: ClickLink, Items: []Item{{URL: "https://media.example.test/storage/a.jpg", Link: "/about"}}}
	view, err := BuildView("g1", &p, nil, nil)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Items[0].Href != "/about" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", view.Items[0].Href)
	}
}

func TestBuildViewExternalLinkUntouched(t *testing.T) {
	p := Props{ClickAction: ClickLink, Items: []Item{{URL: "https://media.example.test/storage/a.jpg", Link: "https://example.com/x"}}}
	view, err := BuildView("g1", &p, nil, siteLinkOf)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Items[0].Href != "https://example.com/x" {
		t.Fatalf("外链不该被改写，实际 %q", view.Items[0].Href)
	}
}
