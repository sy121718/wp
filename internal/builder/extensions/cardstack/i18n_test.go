package cardstack

import (
	"testing"

	"go_wp/pkg/builder/core"
)

func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

func collectionLinkProps() *Props {
	return &Props{
		CollectionSource: "content:article",
		CardTitleField:   "title",
		CardLinkField:    "slug",
		CardLinkPrefix:   "/article/",
	}
}

func TestCollectionCardsLocalizesLinkPrefix(t *testing.T) {
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	ctx.SetSiteLinkResolver(siteLinkOf)

	view, err := BuildView(nodeOf(nil, 0), collectionLinkProps(), ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.Cards) != 2 {
		t.Fatalf("应展开 2 张卡，got %d", len(view.Cards))
	}
	if view.Cards[0].Href != "/en/article/first" {
		t.Fatalf("详情地址应带语言前缀且保留分隔斜杠，实际 %q", view.Cards[0].Href)
	}
}

func TestCollectionCardsWithoutSiteLink(t *testing.T) {
	ctx := &core.RenderContext{Collection: fakeCollection{}}
	view, err := BuildView(nodeOf(nil, 0), collectionLinkProps(), ctx)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if view.Cards[0].Href != "/article/first" {
		t.Fatalf("未注入本地化器时应原样输出，实际 %q", view.Cards[0].Href)
	}
}

func TestLocalizedLinkPrefixKeepsTrailingSlash(t *testing.T) {
	if got := localizedLinkPrefix("/article/", siteLinkOf); got != "/en/article/" {
		t.Fatalf("带尾斜杠的前缀应补回分隔符，实际 %q", got)
	}
	if got := localizedLinkPrefix("/article", siteLinkOf); got != "/en/article" {
		t.Fatalf("不带尾斜杠的前缀不该凭空加一个，实际 %q", got)
	}
	if got := localizedLinkPrefix("", siteLinkOf); got != "" {
		t.Fatalf("空前缀原样返回，实际 %q", got)
	}
}
