package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

func TestBuildSEOHeadRobots(t *testing.T) {
	defaults := builder.BuildSEOHead(builder.SEO{}, "https://x.test/a", "标题", "描述", "", nil)
	if strings.Contains(defaults, "name=\"robots\"") {
		t.Errorf("默认（index/follow）不该输出 robots meta：%s", defaults)
	}
	if !strings.Contains(defaults, "og:type\" content=\"website\"") {
		t.Errorf("默认 og:type 应为 website：%s", defaults)
	}

	none := builder.BuildSEOHead(builder.SEO{RobotsIndex: "noindex", RobotsFollow: "nofollow"},
		"https://x.test/a", "标题", "描述", "", nil)
	if !strings.Contains(none, "<meta name=\"robots\" content=\"noindex,nofollow\">") {
		t.Errorf("noindex,nofollow 未输出：%s", none)
	}

	partial := builder.BuildSEOHead(builder.SEO{RobotsIndex: "noindex"}, "https://x.test/a", "标题", "描述", "", nil)
	if !strings.Contains(partial, "content=\"noindex,follow\"") {
		t.Errorf("缺省项应补 follow：%s", partial)
	}
}

func TestBuildSEOHeadOGTypeFollowsSchemaType(t *testing.T) {
	for _, tc := range []struct{ schema, want string }{
		{"", "website"},
		{"website", "website"},
		{"faq", "website"},
		{"article", "article"},
		{"product", "product"},
		{"Article", "article"},
	} {
		got := builder.BuildSEOHead(builder.SEO{SchemaType: tc.schema}, "https://x.test/a", "标题", "描述", "", nil)
		if !strings.Contains(got, "og:type\" content=\""+tc.want+"\"") {
			t.Errorf("schemaType=%q 时 og:type 应为 %q：%s", tc.schema, tc.want, got)
		}
	}
}
