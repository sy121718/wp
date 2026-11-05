package producthttp

import (
	"strings"
	"testing"

	productservice "go_wp/internal/module/product/service"
)

// TestProductSEOHTMLRendersFacts 验证 Jet 直接展示评分事实并转义内容，不再依赖浏览器字典。
func TestProductSEOHTMLRendersFacts(t *testing.T) {
	for _, tc := range []struct{ profile, label string }{
		{"product", "商品"}, {"landing", "落地页"}, {"guide", "长文指南"}, {"", "默认"}, {"<custom-profile>", "&lt;custom-profile&gt;"},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			data := &productservice.SeoScoreFacts{
				EntityID: "p1", EntityType: "product", ProjectID: "proj-1", Total: 91, ProfileType: tc.profile,
				Title: "<script>bad()</script>", URL: "/a", Description: "说明",
				Sections: []productservice.SeoScoreSection{{Key: "title", Score: 3, Max: 10, Percent: 30, Weight: 0.1234,
					Checks: []productservice.SeoScoreCheck{{Key: "title_present", Score: 3, Max: 5}, {Key: "title_length", Score: 0, Max: 5, Skipped: true}, {Key: "meta_length", Score: 5, Max: 5}}}},
				Duplicates: []productservice.SeoTitleDuplicate{{ID: "p2", Kind: "product", Title: "<img onerror=bad()>", Path: "/b"}},
				Coverage:   productservice.SeoTitleCoverage{Omitted: []productservice.SeoTitleOmission{{Source: "article", Reason: "project_filter_unavailable"}}},
			}
			out := renderAdminTemplate(t, "admin/product/product_seo_panel.html", data)
			for _, want := range []string{`data-product-seo-panel`, `data-seo-total>91</span>`, "优秀", tc.label, "标题标签", "12.34%", "3/5", "为页面填写 SEO 标题", "&lt;script&gt;bad()&lt;/script&gt;", "&lt;img onerror=bad()&gt;", "/b", "重复标题检查未覆盖以下来源", "文章", "</div>"} {
				if !strings.Contains(out, want) {
					t.Fatalf("SEO HTML 缺少 %q：%s", want, out)
				}
			}
			for _, forbidden := range []string{"product-seo-dictionary", "<script>", "<img ", "删掉冗余词", "补齐到 150-160"} {
				if strings.Contains(out, forbidden) {
					t.Fatalf("SEO HTML 不应包含 %q：%s", forbidden, out)
				}
			}
		})
	}
}

// TestProductSEOHTMLEmptyFacts 验证零分、空 SERP 与空明细仍有真实 HTML。
func TestProductSEOHTMLEmptyFacts(t *testing.T) {
	out := renderAdminTemplate(t, "admin/product/product_seo_panel.html", &productservice.SeoScoreFacts{})
	for _, want := range []string{`data-seo-total>0</span>`, "缺失", "未填写标题", "线上路径未知", "未填写描述"} {
		if !strings.Contains(out, want) {
			t.Fatalf("SEO 空态缺少 %q：%s", want, out)
		}
	}
	if strings.Contains(out, `data-seo-duplicates`) || strings.Contains(out, `data-seo-coverage`) {
		t.Fatalf("空重复项与完整覆盖不应显示警告：%s", out)
	}
}
