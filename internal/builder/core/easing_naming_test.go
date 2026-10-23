package core

// easing_naming_test.go — 动效**命名与档位**的契约。
//
// 两条被守的规则，都是「读代码时才会发现不对劲」的那类：
//
//  1. 入场的名字必须含 in。sky-fade-up / sky-zoom-out / sky-slide-up 里没有 in，
//     分不清入场还是出场 —— 而这一整组全是入场（from 有初值、to 归位）。
//  2. 缓动要有两类：带过冲的（linear() 弹簧）与单调的（cubic-bezier）。
//     只有前者时，想要「平稳减速」就只能退回 CSS 关键字 ease/ease-in，
//     那是浏览器默认手感，与本项目视觉语言对不上。

import (
	"sort"
	"strings"
	"testing"
)

// TestEntranceNamesCarryInSuffix 入场词汇要么是空、要么含 "in"。
//
// 存量兼容：旧名（无 -in）仍在白名单里，否则已发布内容的文档会解析失败。
// 所以这条判据分两档：**新名必须含 in**，旧名登记在 legacyEntranceNames 里。
func TestEntranceNamesCarryInSuffix(t *testing.T) {
	// 旧名白名单（只减不增：新加词汇一律用 -in 名）。
	// 这份清单同时是「哪些存量值还在被支持」的答案，改它等于改兼容承诺。
	legacy := map[string]bool{
		"fade-up": true, "fade-down": true, "fade-left": true, "fade-right": true,
		"zoom-out": true,
		"slide-up": true, "slide-down": true, "slide-left": true, "slide-right": true,
		"flip-x": true, "flip-y": true,
	}

	var offenders, legacyPresent []string
	for name := range allowedEntrance {
		if name == "" {
			continue
		}
		if legacy[name] {
			legacyPresent = append(legacyPresent, name)
			continue
		}
		if !strings.Contains(name, "in") {
			offenders = append(offenders, name)
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("入场词汇缺少 \"in\"（入场/出场分不清）：%v", offenders)
	}

	// 旧名必须真的还在白名单里 —— 否则存量文档会解析失败，而失败形态是
	// 「静默跳过该动效」，产物少一段动画但不报错。
	var missing []string
	for name := range legacy {
		if !allowedEntrance[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("旧入场名被删了：%v —— 存量文档里存的是这些字符串，删掉等于让它们的产物悄悄少一段动画。"+
			"若要退役，先做数据迁移", missing)
	}
	if len(legacyPresent) != len(legacy) {
		t.Errorf("legacy 清单与实际白名单不一致：清单 %d 条，白名单里只找到 %d 条", len(legacy), len(legacyPresent))
	}
}

// TestNewEntranceNamesProduceSameKeyframes 每个 -in 新名都必须有对应关键帧，
// 且与它的旧名产出**同一份帧体**（只差名字）。
//
// 「加别名」这件事最容易出的错是只改了白名单、忘了加关键帧：名字合法、校验通过，
// 但浏览器拿不到 @keyframes —— 元素直接以终态出现，动画静默消失。
func TestNewEntranceNamesProduceSameKeyframes(t *testing.T) {
	pairs := map[string]string{
		"fade-in-up":     "fade-up",
		"fade-in-down":   "fade-down",
		"fade-in-left":   "fade-left",
		"fade-in-right":  "fade-right",
		"zoom-in-out":    "zoom-out",
		"slide-in-up":    "slide-up",
		"slide-in-down":  "slide-down",
		"slide-in-left":  "slide-left",
		"slide-in-right": "slide-right",
	}
	names := make([]string, 0, len(pairs))
	for n := range pairs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, newName := range names {
		oldName := pairs[newName]
		newCSS := KeyframeCSS([]string{EffectKeyframeName(KindEntrance, newName)})
		if newCSS == "" {
			t.Errorf("%s 没有关键帧 —— 白名单放行了但浏览器拿不到 @keyframes，动画会静默消失", newName)
			continue
		}
		oldCSS := KeyframeCSS([]string{EffectKeyframeName(KindEntrance, oldName)})
		if oldCSS == "" {
			t.Errorf("%s（旧名 %s 的关键帧）不见了 —— 存量文档会失去动画", oldName, oldName)
			continue
		}
		// 帧体相同：把名字换掉后应当逐字节一致。
		a := strings.ReplaceAll(newCSS, EffectKeyframeName(KindEntrance, newName), "X")
		b := strings.ReplaceAll(oldCSS, EffectKeyframeName(KindEntrance, oldName), "X")
		if a != b {
			t.Errorf("%s 与旧名 %s 的帧体不同：\n新：%s\n旧：%s\n（别名必须产出同一份视觉结果）", newName, oldName, newCSS, oldCSS)
		}
	}
}

// TestEntranceEasingHasBothFamilies 缓动必须同时有「有回弹」与「无回弹」两类。
func TestEntranceEasingHasBothFamilies(t *testing.T) {
	var spring, bezier []string
	for name := range allowedEntranceEasing {
		if name == "" {
			continue
		}
		if _, ok := StandardEasingNames[name]; ok {
			bezier = append(bezier, name)
			continue
		}
		spring = append(spring, name)
	}
	sort.Strings(spring)
	sort.Strings(bezier)

	if len(spring) < 4 {
		t.Errorf("弹簧档只有 %d 条（期望 spring/soft/bouncy/classic 四条）：%v", len(spring), spring)
	}
	if len(bezier) < 4 {
		t.Errorf("标准档只有 %d 条（期望 standard/decelerate/accelerate/emphasized 四条）：%v", len(bezier), bezier)
	}
	// 标准档的曲线必须是 cubic-bezier；弹簧档必须是 linear() —— 两类不许混。
	for name, curve := range StandardEasingNames {
		if !strings.HasPrefix(curve, "cubic-bezier(") {
			t.Errorf("标准档 %s 的曲线不是 cubic-bezier：%q", name, curve)
		}
		if !allowedEntranceEasing[name] {
			t.Errorf("标准档 %s 不在 allowedEntranceEasing 里 —— 声明了却用不了", name)
		}
	}
	for _, curve := range []string{SpringStandardCurve, SpringSoftCurve, SpringBouncyCurve} {
		if !strings.HasPrefix(curve, "linear(") {
			t.Errorf("弹簧曲线应为 linear() 采样（cubic-bezier 无法过冲）：%q", curve)
		}
	}
}

// TestStandardEasingApplied 选标准档时，产物里必须是 cubic-bezier 而不是弹簧。
func TestStandardEasingApplied(t *testing.T) {
	for name, curve := range StandardEasingNames {
		b := &CSSBuckets{}
		CompileInteraction(".t", InteractionProps{Entrance: "fade-in", EntranceEasing: name}, b)
		css := b.String()
		if !strings.Contains(css, "animation-timing-function: "+curve) {
			t.Errorf("缓动 %s 的产物里没有 %s：\n%s", name, curve, css)
		}
		if strings.Contains(css, "linear(") {
			t.Errorf("缓动 %s 走到了弹簧分支（产物里有 linear()）：\n%s", name, css)
		}
	}
}
