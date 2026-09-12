package core

import (
	"sort"
	"strings"
	"testing"
)

// TestInteractionWhitelistMatchesKeyframes 三个动效白名单的每个名字都必须有对应关键帧。
//
// 名字到关键帧的对应是一条**约定**：入场 `sky-<name>`、循环 `sky-loop-<name>`、
// 滚动叙事 `sky-story-<name>`。而白名单（groups.go）、拼名（CompileInteraction）、
// 关键帧源（keyframes/*.css）是三处各自手写维护的东西，没有任何机制保证它们同步。
//
// 三者一旦错位，产物里就会出现「有 animation 引用、没有 @keyframes 定义」的 CSS：
// 白名单校验通过、构建成功、产物字节合法，页面上那个动效就是不动 —— 没有任何地方报错。
// 这条用例把约定变成构建期强制：往白名单加名字却忘了写关键帧，测试立刻失败。
//
// 反向（关键帧源里有、白名单没引用）**不做断言**：那是允许的 —— 效果基本库里的
// sky-bg-flow / sky-border-flow 就由 BackgroundFlowDecls / BorderFlowAngleProperty
// 走另一条路径激活，不经交互白名单。
func TestInteractionWhitelistMatchesKeyframes(t *testing.T) {
	have := map[string]bool{}
	for _, k := range keyframesCatalog {
		have[k.Name] = true
	}
	// 走 EffectKeyframeName 取名，而不是在测试里另拼一份前缀 ——
	// 那样等于把「名字 → 关键帧」的规则又抄了一遍，抄错就测不出东西。
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
		sort.Strings(names) // 稳定顺序，失败信息便于逐条对照
		for _, name := range names {
			kf := EffectKeyframeName(kd.kind, name)
			if !have[kf] {
				t.Errorf("%s白名单里的 %q 没有对应的关键帧 %q —— 产物会引用一个不存在的动画，页面上只表现为「不动」",
					kd.label, name, kf)
			}
		}
	}
}

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
