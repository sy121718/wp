package cardstack

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestEffectNamesAreInVocab(t *testing.T) {
	effects := []string{"fade", "zoom", "flip", "bounce", "back", "rotate", "light", "roll", "jack"}

	dirs := []string{"", slideDirectionHorizontal}
	for _, eff := range effects {
		for _, dir := range dirs {
			kf := slideEffectKeyframe(&Props{SlideEffect: eff, SlideDirection: dir})
			if kf == "" {
				t.Errorf("切换效果 %q（方向 %q）没有产出关键帧名", eff, dir)
				continue
			}
			if name := strings.TrimPrefix(kf, "sky-"); !core.EffectAllowed(core.KindEntrance, name) {
				t.Errorf("切换效果 %q 用了不在入场词汇表里的名字 %q", eff, name)
			}
		}
	}

	for _, v := range []string{"glow", "flash"} {
		kf := loopEffectKey(v)
		if name := strings.TrimPrefix(kf, "sky-loop-"); !core.EffectAllowed(core.KindLoop, name) {
			t.Errorf("循环效果 %q 用了不在循环词汇表里的名字 %q", v, name)
		}
	}
	if loopEffectKey("") != "" || loopEffectKey("pulse") != "" {
		t.Error("只有 glow / flash 允许作为逐卡循环效果")
	}
}
