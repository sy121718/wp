package cardstack

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestEffectNamesAreInVocab 逐卡错落用的效果名必须都在 core 的动效词汇表里。
//
// cardstack 把属性值（fade / zoom / flip…）映射成通用动效词汇名，再交给
// core.EffectKeyframeName 拼关键帧名。**前缀**由 core 保证只有一处定义，
// 但名字本身是这里写死的：拼错一个字母就只会得到「引用了不存在的关键帧」，
// 动画不动、构建与校验都不报错。
//
// 这条与 core 的 TestInteractionWhitelistMatchesKeyframes 互补：那条管
// 「白名单 ↔ 关键帧源」，这条管「组件里写死的词汇名 ↔ 白名单」。
func TestEffectNamesAreInVocab(t *testing.T) {
	effects := []string{"fade", "zoom", "flip", "bounce", "back", "rotate", "light", "roll", "jack"}
	// 空串＝缺省纵向（没有单独的 vertical 常量）。
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

	// 循环：只允许不占用 transform 的两条（glow 走 filter、flash 走 opacity），
	// 其余循环词汇都改 transform，会顶掉卡片的位移与缩放。
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
