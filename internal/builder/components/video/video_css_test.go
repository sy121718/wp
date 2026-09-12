package video

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestVideoCSSRatio 宽高比映射到 padding-top 百分比，自适应档位不产出占位高度。
func TestVideoCSSRatio(t *testing.T) {
	cases := []struct {
		name  string
		ratio string
		pad   string
	}{
		{"缺省 16:9", "", "56.25%"},
		{"4:3", "4:3", "75%"},
		{"1:1", "1:1", "100%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b core.CSSBuckets
			compileCSS("t", &Props{Ratio: c.ratio}, &b)
			out := b.String()
			if !strings.Contains(out, "padding-top: "+c.pad) {
				t.Errorf("产物缺少 padding-top: %s\n%s", c.pad, out)
			}
		})
	}

	// auto：不产出占位高度（高度由内容决定）。
	var b core.CSSBuckets
	compileCSS("t", &Props{Ratio: "auto"}, &b)
	if strings.Contains(b.String(), "padding-top") {
		t.Errorf("auto 不该产出 padding-top:\n%s", b.String())
	}
}

// TestVideoCSSAlignAndOptional 对齐三条 margin 恒产出（居中也是显式声明）；全宽与圆角可选。
func TestVideoCSSAlignAndOptional(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{"margin-left: auto", "margin-right: auto", "max-width: 100%", "border-radius: inherit"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	// 比例框与播放器本体是两条独立规则（播放器本体绝对定位铺满）。
	if !strings.Contains(out, ".sky-c-t .sky-video-frame {") {
		t.Errorf("缺少比例框规则:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t .sky-video-frame iframe, .sky-c-t .sky-video-frame video {") {
		t.Errorf("缺少播放器本体规则:\n%s", out)
	}

	var l core.CSSBuckets
	compileCSS("t", &Props{Align: "left", FullWidth: true, Radius: "12px"}, &l)
	out2 := l.String()
	for _, want := range []string{"margin-left: 0", "margin-right: auto", "border-radius: 12px"} {
		if !strings.Contains(out2, want) {
			t.Errorf("产物缺少 %q\n%s", want, out2)
		}
	}
}
