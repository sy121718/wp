package shapedivider

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestShapedividerCSSDefaults 三端高度的缺省值与自定义覆盖。
func TestShapedividerCSSDefaults(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {",
		".sky-c-t svg {\n  height: 120px;",
		".sky-c-t svg {\n  height: 90px;",
		".sky-c-t svg {\n  height: 64px;",
		"line-height: 0",
		"overflow: hidden",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	var c core.CSSBuckets
	compileCSS("t", &Props{Height: Height{Desktop: "200px", Tablet: "150px", Mobile: "100px"}}, &c)
	out2 := c.String()
	for _, want := range []string{"height: 200px", "height: 150px", "height: 100px"} {
		if !strings.Contains(out2, want) {
			t.Errorf("自定义高度未生效 %q\n%s", want, out2)
		}
	}
}

// TestShapedividerCSSFlip 镜像的四种组合（含不镜像时整条 transform 不产出）。
func TestShapedividerCSSFlip(t *testing.T) {
	cases := []struct {
		name  string
		flipX bool
		flipY bool
		want  string
	}{
		{"不镜像", false, false, ""},
		{"水平镜像", true, false, "transform: scaleX(-1)"},
		{"垂直镜像", false, true, "transform: scaleY(-1)"},
		{"双向镜像", true, true, "transform: scaleX(-1) scaleY(-1)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b core.CSSBuckets
			compileCSS("t", &Props{FlipX: c.flipX, FlipY: c.flipY}, &b)
			out := b.String()
			if c.want == "" {
				if strings.Contains(out, "transform") {
					t.Errorf("不镜像却产出了 transform:\n%s", out)
				}
				return
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("产物缺少 %q\n%s", c.want, out)
			}
		})
	}
}

// TestShapedividerCSSDrift 漂移动画与关键帧必须成对出现。
//
// 只产出 animation 不产出 @keyframes → 动画静默不执行；只产出关键帧不产出 animation →
// 白背一段没人引用的字节。两者由同一个 drift 变量控制，这条测试保证它们不脱节。
func TestShapedividerCSSDrift(t *testing.T) {
	var off core.CSSBuckets
	compileCSS("t", &Props{}, &off)
	out := off.String()
	if strings.Contains(out, "animation:") || strings.Contains(out, "@keyframes") {
		t.Errorf("未开漂移却产出了动画或关键帧:\n%s", out)
	}

	var on core.CSSBuckets
	compileCSS("t", &Props{Animate: AnimDrift}, &on)
	out2 := on.String()
	for _, want := range []string{
		".sky-c-t .sd-l2 {\n  animation: sky-sd-drift 14s ease-in-out infinite alternate;",
		".sky-c-t .sd-l3 {\n  animation: sky-sd-drift 22s ease-in-out infinite alternate-reverse;",
		"@keyframes sky-sd-drift {",
		"transform: translateX(-60px)",
	} {
		if !strings.Contains(out2, want) {
			t.Errorf("产物缺少 %q\n%s", want, out2)
		}
	}
}
