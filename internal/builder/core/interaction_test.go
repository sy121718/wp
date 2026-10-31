package core

import (
	"sort"
	"strings"
	"testing"
)

func TestInteractionWhitelistMatchesKeyframes(t *testing.T) {
	have := map[string]bool{}
	for _, k := range keyframesCatalog {
		have[k.Name] = true
	}

	kinds := []struct {
		kind  EffectKind
		label string
	}{
		{KindEntrance, "入场"},
		{KindLoop, "循环"},
		{KindStory, "滚动叙事"},
	}
	for _, kd := range kinds {
		names := make([]string, 0, len(effectNames[kd.kind]))
		for name := range effectNames[kd.kind] {
			if name != "" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			kf := EffectKeyframeName(kd.kind, name)
			if !have[kf] {
				t.Errorf("%s白名单里的 %q 没有对应的关键帧 %q —— 产物会引用一个不存在的动画，页面上只表现为「不动」",
					kd.label, name, kf)
			}
		}
	}
}

func TestInteractionLoopDrift(t *testing.T) {
	if err := ValidateInteraction(InteractionProps{LoopEffect: "drift"}); err != nil {
		t.Fatalf("drift 应在循环动效白名单内: %v", err)
	}

	b := &CSSBuckets{}
	CompileInteraction(".t", InteractionProps{LoopEffect: "drift"}, b)
	css := b.String()
	for _, want := range []string{
		"animation: sky-loop-drift 2.4s ease-in-out infinite",
		"@keyframes sky-loop-drift",
		"translateX(-8px)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}

	b2 := &CSSBuckets{}
	CompileInteraction(".t2", InteractionProps{LoopEffect: "float"}, b2)
	if strings.Contains(b2.String(), "sky-loop-drift") {
		t.Errorf("未引用 drift 时不应输出其 keyframes")
	}
}

func TestInteractionLoopAll(t *testing.T) {
	all := []string{
		"pulse", "float", "drift", "shake", "jello", "heartbeat", "blob",
		"flash", "rubber-band", "swing", "tada", "wobble", "head-shake", "bounce",
		"glow", "spin",
	}
	for _, fx := range all {
		t.Run(fx, func(t *testing.T) {
			if err := ValidateInteraction(InteractionProps{LoopEffect: fx}); err != nil {
				t.Fatalf("%s 应在循环动效白名单内: %v", fx, err)
			}
			b := &CSSBuckets{}
			CompileInteraction(".t", InteractionProps{LoopEffect: fx}, b)
			css := b.String()
			anim := "animation: sky-loop-" + fx + " 2.4s ease-in-out infinite"
			if !strings.Contains(css, anim) {
				t.Errorf("CSS 缺少 %q\n%s", anim, css)
			}
			if !strings.Contains(css, "@keyframes sky-loop-"+fx) {
				t.Errorf("CSS 缺少 @keyframes sky-loop-%s\n%s", fx, css)
			}
		})
	}

	if err := ValidateInteraction(InteractionProps{LoopEffect: "pop"}); err == nil {
		t.Errorf("非法循环动效应被拒绝")
	}
}
