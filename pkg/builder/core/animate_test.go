package core

import (
	"strings"
	"testing"
)

func TestAnimateEntranceAll(t *testing.T) {
	for _, kf := range keyframesAnimate {
		name0 := kf.Name
		if strings.HasPrefix(name0, "sky-loop-") {
			continue
		}
		name := strings.TrimPrefix(name0, "sky-")
		t.Run(name, func(t *testing.T) {
			if err := ValidateInteraction(InteractionProps{Entrance: name}); err != nil {
				t.Fatalf("%s 应在入场白名单内: %v", name, err)
			}
			b := &CSSBuckets{}
			CompileInteraction(".t", InteractionProps{Entrance: name}, b)
			css := b.String()
			if !strings.Contains(css, "animation: "+name0) {
				t.Errorf("CSS 缺少 animation 声明\n%s", css)
			}
			if !strings.Contains(css, "@keyframes "+name0) {
				t.Errorf("CSS 缺少 @keyframes %s\n%s", name0, css)
			}
		})
	}
}

func TestAnimateLoopAll(t *testing.T) {
	for _, kf := range keyframesAnimate {
		name0 := kf.Name
		if !strings.HasPrefix(name0, "sky-loop-") {
			continue
		}
		name := strings.TrimPrefix(name0, "sky-loop-")
		t.Run(name, func(t *testing.T) {
			if err := ValidateInteraction(InteractionProps{LoopEffect: name}); err != nil {
				t.Fatalf("%s 应在循环白名单内: %v", name, err)
			}
			b := &CSSBuckets{}
			CompileInteraction(".t", InteractionProps{LoopEffect: name}, b)
			css := b.String()
			if !strings.Contains(css, "animation: "+name0+" 2.4s ease-in-out infinite") {
				t.Errorf("CSS 缺少循环声明\n%s", css)
			}
			if !strings.Contains(css, "@keyframes "+name0) {
				t.Errorf("CSS 缺少 @keyframes %s\n%s", name0, css)
			}
		})
	}
}
