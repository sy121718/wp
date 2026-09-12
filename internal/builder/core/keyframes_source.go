package core

// keyframes_source.go — 动效关键帧的 CSS 源（keyframes/*.css）读取。
//
// 此前 63 条关键帧的 CSS 文本以 Go 字符串字面量散在 css.go（内置组）与 keyframes_animate.go（拆解组）里，
// 改一个动画要去 Go 文件里翻大段 CSS。现在它们是真 CSS 文件：
//
//     internal/builder/core/keyframes/builtin.css   内置动效（入场 / 循环 / 用法 FX / 滚动叙事）
//     internal/builder/core/keyframes/animate.css   Animate.css v4 拆解（MIT）
//
// **只迁 CSS 文本，不迁判定逻辑**：动效词汇表白名单（ValidateInteraction）、激活（NeedKeyframes）、
// 无障碍（prefers-reduced-motion）都留在 Go —— 它们承担的是安全模型与语义，不是样式。
//
// 顺序即产物输出顺序（String() 按 keyframesCatalog 遍历），所以文件内顺序是确定性的一部分，
// 解析必须保序、保字节。

import (
	_ "embed"
	"fmt"
	"strings"
)

// ParseKeyframeCSS 从 CSS 源提取关键帧（名 → 完整规则体）。
//
// 按 `@keyframes ` 边界切分，而不是花括号计数配对：迁出的历史源里存在多余的闭合花括号
// （浏览器容忍，但计数法会在那里截短），切片法能原样保留。迁移要求字节保真，
// 任何「顺手修正」都会让迁移前后无法逐字节对比。
func ParseKeyframeCSS(src string) ([]Keyframe, error) {
	// 去掉文件头注释块，避免把说明文字当成规则。
	body := src
	if i := strings.Index(body, "*/"); strings.HasPrefix(strings.TrimSpace(body), "/*") && i >= 0 {
		body = body[i+2:]
	}

	var out []Keyframe
	const marker = "@keyframes "
	for {
		idx := strings.Index(body, marker)
		if idx < 0 {
			break
		}
		body = body[idx:]
		// 下一段的起点：下一个 @keyframes（找不到就到结尾）。
		next := strings.Index(body[len(marker):], marker)
		var seg string
		if next < 0 {
			seg = body
			body = ""
		} else {
			seg = body[:len(marker)+next]
			body = body[len(marker)+next:]
		}
		seg = strings.TrimRight(seg, "\n")
		if seg == "" {
			continue
		}
		name, err := keyframeNameOf(seg)
		if err != nil {
			return nil, err
		}
		out = append(out, Keyframe{Name: name, CSS: seg})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有解析到任何 @keyframes")
	}
	return out, nil
}

// keyframeNameOf 从一段规则里取出 @keyframes 的名字。
func keyframeNameOf(seg string) (string, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(seg, "@keyframes "))
	brace := strings.Index(rest, "{")
	if brace < 0 {
		return "", fmt.Errorf("@keyframes 缺少花括号: %q", firstLine(seg))
	}
	name := strings.TrimSpace(rest[:brace])
	if name == "" {
		return "", fmt.Errorf("@keyframes 名字为空: %q", firstLine(seg))
	}
	return name, nil
}

// firstLine 取首行，用于错误信息（避免把整段 CSS 塞进日志）。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

//go:embed keyframes/builtin.css
var builtinKeyframesSource string

//go:embed keyframes/animate.css
var animateKeyframesSource string

// keyframesBuiltin 内置动效关键帧（入场 / 循环 / 用法 FX / 滚动叙事）。
// 切片顺序即输出顺序，确定性内建于文件顺序，无独立顺序表。
var keyframesBuiltin = mustParseKeyframes("keyframes/builtin.css", builtinKeyframesSource)

// keyframesAnimate Animate.css v4（MIT）效果拆解配方。
var keyframesAnimate = mustParseKeyframes("keyframes/animate.css", animateKeyframesSource)

// mustParseKeyframes 解析关键帧源；失败在 init 期 panic —— 构建期缺陷必须尽早暴露，
// 静默得到一个空表会让产物里的动画「莫名消失」，比启动失败难查得多。
func mustParseKeyframes(file, src string) []Keyframe {
	ks, err := ParseKeyframeCSS(src)
	if err != nil {
		panic("core: 解析 " + file + " 失败: " + err.Error())
	}
	return ks
}
