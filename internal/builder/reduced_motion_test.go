package builder

import (
	"strings"
	"testing"
)

// TestReducedMotionCSS 减弱动态效果：无条件输出 prefers-reduced-motion 块。
func TestReducedMotionCSS(t *testing.T) {
	off := &ThemeSettings{}
	css := off.ReducedMotionCSS()
	for _, want := range []string{"@media (prefers-reduced-motion: reduce)", "animation-duration: 0.01ms !important", "transition-duration: 0.01ms !important"} {
		if !strings.Contains(css, want) {
			t.Errorf("无障碍块缺少 %q\n%s", want, css)
		}
	}
	if (*ThemeSettings)(nil).ReducedMotionCSS() == "" {
		t.Error("nil 主题也应输出无障碍块")
	}
}
