package cardstack

// i18n_test.go — 卡片堆叠链接前缀的站内本地化（审计 I18N-015）。
//
// 本组件按「前缀 + 字段值」直接拼接，**尾斜杠是分隔符**：本地化会规范化路径（去掉尾斜杠），
// 必须按原前缀的形状补回去，否则 /article/ 会被拼成 /en/articlefirst。
// 同时只本地化前缀，CMS 字段值（slug）原样参与拼接。

import (
	"testing"

	"go_wp/internal/builder/core"
)

// siteLinkOf 模拟装配层注入的本地化器：只对站内路径加前缀。
func siteLinkOf(p string) string {
	if len(p) > 0 && p[0] == '/' && !(len(p) > 1 && p[1] == '/') {
		return "/en" + p
	}
	return p
}

// collectionLinkProps 集合模式下的链接字段映射（与既有 TestCollectionCards 同形）。
func collectionLinkProps() *Props {
	return &Props{
		CollectionSource: "content:article",
		CardTitleField:   "title",
		CardLinkField:    "slug",
		CardLinkPrefix:   "/article/",
	}
}

// TestCollectionCardsLocalizesLinkPrefix 作者填的站内前缀按语言加前缀，且尾斜杠不丢。
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

// TestCollectionCardsWithoutSiteLink 未注入本地化器时链接与接入前逐字一致。
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

// TestLocalizedLinkPrefixKeepsTrailingSlash 尾斜杠形状按作者原样保留（前缀是唯一被本地化的部分）。
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
