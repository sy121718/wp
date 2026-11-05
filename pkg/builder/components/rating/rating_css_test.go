package rating

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestRatingCSSRules(t *testing.T) {
	var b core.CSSBuckets
	if err := core.ApplyComponentCSS(&b, ".sky-c-t", ratingCSS); err != nil {
		t.Fatalf("解析 rating.css 失败: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		".sky-c-t {",
		"display: inline-flex",
		"gap: var(--sky-space-1, 2px)",
		"color: var(--sky-c-warning, #f59e0b)",
		".sky-c-t .sky-star {",
		"width: 1.25em",
		".sky-c-t .sky-star svg {",
		"height: 100%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if strings.Contains(out, "&") {
		t.Errorf("产物里残留未替换的 &:\n%s", out)
	}
}
