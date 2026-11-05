package builder

import (
	"strings"
	"testing"
)

func TestVerifyAnimationRefsAcceptsDefined(t *testing.T) {
	css := "@keyframes sky-fade-in-up {\n  from { opacity: 0 }\n}\n.sky-c-a {\n  animation: sky-fade-in-up 0.6s ease backwards;\n}"
	if err := verifyAnimationRefs(css); err != nil {
		t.Fatalf("引用了已定义的关键帧不该报错: %v", err)
	}
}

func TestVerifyAnimationRefsRejectsUndefined(t *testing.T) {

	css := "@keyframes sky-loop-drift {\n  from { transform: none }\n}\n.sky-c-a {\n  animation: sky-fade-in-up 0.6s ease backwards, sky-loop-drift 2.4s ease-in-out infinite;\n}"
	err := verifyAnimationRefs(css)
	if err == nil {
		t.Fatal("引用未定义的关键帧必须报错")
	}
	if !strings.Contains(err.Error(), "sky-fade-in-up") {
		t.Errorf("报错应指出缺哪一个: %v", err)
	}
	if strings.Contains(err.Error(), "sky-loop-drift") {
		t.Errorf("已定义的那个不该出现在报错里: %v", err)
	}
}

func TestVerifyAnimationRefsIgnoresNonNamespace(t *testing.T) {
	css := ".sky-c-a {\n  animation: none;\n}\n" +
		".sky-c-b {\n  animation: 2s ease;\n}\n" +
		"// 插件自己的命名空间，不归本项目管\n.sky-c-c {\n  animation: plugin-fx 1s linear;\n}\n" +
		".sky-c-d {\n  animation-timeline: view();\n  animation-range: entry 0% exit 100%;\n}"
	if err := verifyAnimationRefs(css); err != nil {
		t.Fatalf("不该报错（误报会让插件样式无法落地）: %v", err)
	}
}

func TestVerifyAnimationRefsFindsNameAnywhere(t *testing.T) {
	css := ".sky-c-a {\n  animation: 2s ease-in-out infinite sky-loop-glow;\n}"
	if err := verifyAnimationRefs(css); err == nil {
		t.Fatal("名字出现在简写中间也要被查到")
	}
}
