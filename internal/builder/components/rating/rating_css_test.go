package rating

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestRatingCSSRules 迁移到 rating.css 后逐条核对关键规则仍在。
//
// 样式从 compileCSS 的 3 处 b.Add 迁成 CSS 文件，中间多了一层解析（作用域替换 / 桶划分）。
// 解析器写错、选择器替换错 —— 任何一处出错都让产物「悄悄少了样式」，页面上只表现为「有点不对劲」。
func TestRatingCSSRules(t *testing.T) {
	var b core.CSSBuckets
	if err := core.ApplyComponentCSS(&b, ".sky-c-t", ratingCSS); err != nil {
		t.Fatalf("解析 rating.css 失败: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		".sky-c-t {",
		"display: inline-flex",
		"gap: 2px",
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
	// 作用域必须生效：残留 & 说明替换没做。
	if strings.Contains(out, "&") {
		t.Errorf("产物里残留未替换的 &:\n%s", out)
	}
}
