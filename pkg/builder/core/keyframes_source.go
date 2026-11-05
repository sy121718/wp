package core

import (
	_ "embed"
	"fmt"
	"strings"
)

func ParseKeyframeCSS(src string) ([]Keyframe, error) {

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

var keyframesBuiltin = mustParseKeyframes("keyframes/builtin.css", builtinKeyframesSource)

var keyframesAnimate = mustParseKeyframes("keyframes/animate.css", animateKeyframesSource)

func mustParseKeyframes(file, src string) []Keyframe {
	ks, err := ParseKeyframeCSS(src)
	if err != nil {
		panic("core: 解析 " + file + " 失败: " + err.Error())
	}
	return ks
}
