package core

import (
	"strings"
	"testing"
)

// TestInteractionLoopDrift 通用循环动效 drift：白名单放行 + 编译输出 + keyframes 按需激活。
// drift 属于「效果基本库」通用词汇：任意组件经 Advanced.Interaction.LoopEffect 自由选用，
// CompileInteraction 统一编译，未引用的组件零 CSS 输出（按需编译不变式）。
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

	// 未引用 drift 时不输出其 keyframes。
	b2 := &CSSBuckets{}
	CompileInteraction(".t2", InteractionProps{LoopEffect: "float"}, b2)
	if strings.Contains(b2.String(), "sky-loop-drift") {
		t.Errorf("未引用 drift 时不应输出其 keyframes")
	}
}

// TestInteractionLoopAll 全部循环动效词汇：白名单放行 + animation 声明与 keyframes 一一激活。
// shake/jello/heartbeat 为效果基本库扩充（参考 Animate.css/CSShake 自研简化帧）。
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

	// 非法词汇拒绝。
	if err := ValidateInteraction(InteractionProps{LoopEffect: "pop"}); err == nil {
		t.Errorf("非法循环动效应被拒绝")
	}
}
