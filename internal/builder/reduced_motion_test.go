package builder

import (
	"strings"
	"testing"
)

// TestReducedMotionCSS 减弱动态效果：默认关闭零输出；开启输出无障碍块。
func TestReducedMotionCSS(t *testing.T) {
	off := &ThemeSettings{}
	if off.ReducedMotionCSS() != "" {
		t.Errorf("默认关闭时应零输出")
	}
	nilSettings := (*ThemeSettings)(nil)
	if nilSettings.ReducedMotionCSS() != "" {
		t.Errorf("nil 主题应零输出")
	}
	on := &ThemeSettings{Motion: ThemeMotion{ReducedMotion: true}}
	css := on.ReducedMotionCSS()
	for _, want := range []string{"@media (prefers-reduced-motion: reduce)", "animation-duration: 0.01ms !important", "transition-duration: 0.01ms !important"} {
		if !strings.Contains(css, want) {
			t.Errorf("无障碍块缺少 %q\n%s", want, css)
		}
	}
}
