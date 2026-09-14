package unit

// seo_head_robots_test.go — robots 指令与 og:type 的输出规则。
//
// 两者都不改变默认产物字节：robots 两项均为默认（index/follow）时不输出 meta，
// og:type 在 schemaType 为空 / website / faq 时仍是 website。

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

	// 只设一项时另一项补默认值：只写 noindex 不该丢掉 follow 语义。
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
		{"Article", "article"}, // 大小写不敏感
	} {
		got := builder.BuildSEOHead(builder.SEO{SchemaType: tc.schema}, "https://x.test/a", "标题", "描述", "", nil)
		if !strings.Contains(got, "og:type\" content=\""+tc.want+"\"") {
			t.Errorf("schemaType=%q 时 og:type 应为 %q：%s", tc.schema, tc.want, got)
		}
	}
}
