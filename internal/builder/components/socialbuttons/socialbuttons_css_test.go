package socialbuttons

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func socialCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// TestSocialCSSBrandOrder 品牌色规则按 brandOrder 的固定顺序展开。
//
// 顺序进产物字节，所以不能吃 map 遍历顺序 —— 那会让同一份文档每次构建产出不同 CSS，
// 直接破坏确定性构建这条不变量。
func TestSocialCSSBrandOrder(t *testing.T) {
	out := socialCSSFor(&Props{})
	prev := -1
	count := 0
	for _, platform := range brandOrder {
		if _, ok := brandColors[platform]; !ok {
			continue
		}
		idx := strings.Index(out, ".sky-c-t a[aria-label=\""+platform+"\"] {")
		if idx < 0 {
			t.Errorf("缺少平台 %s 的品牌色规则:\n%s", platform, out)
			continue
		}
		if idx < prev {
			t.Errorf("平台 %s 的顺序与 brandOrder 不一致（确定性构建会被破坏）", platform)
		}
		prev = idx
		count++
	}
	if count < 20 {
		t.Errorf("品牌色规则只展开了 %d 条，明显少了", count)
	}
}

// TestSocialCSSCSColorModes 三种配色互斥：只命中一个分支。
func TestSocialCSSCSColorModes(t *testing.T) {
	brand := socialCSSFor(&Props{})
	if strings.Contains(brand, "color: #6b7280;") {
		t.Errorf("品牌模式不该产出单色配色:\n%s", brand)
	}
	if strings.Contains(brand, "background: rgba(0,0,0,.06);") {
		t.Errorf("品牌模式不该产出单色底:\n%s", brand)
	}

	mono := socialCSSFor(&Props{Color: ColorMono})
	if !strings.Contains(mono, "color: #6b7280;") {
		t.Errorf("单色配色缺失:\n%s", mono)
	}
	if strings.Contains(mono, "aria-label=") {
		t.Errorf("单色模式不该产出品牌色规则:\n%s", mono)
	}

	custom := socialCSSFor(&Props{Color: ColorCustom, CustomColor: "#123456"})
	if !strings.Contains(custom, "color: #123456;") {
		t.Errorf("自定义色缺失:\n%s", custom)
	}
	fallback := socialCSSFor(&Props{Color: ColorCustom})
	if !strings.Contains(fallback, "color: #2563eb;") {
		t.Errorf("未填自定义色时应兜底 #2563eb:\n%s", fallback)
	}
}

// TestSocialCSSShapeAndSize 形状三档只差一个圆角值，尺寸贯穿宽高与字号。
func TestSocialCSSShapeAndSize(t *testing.T) {
	if !strings.Contains(socialCSSFor(&Props{}), "border-radius: 999px;") {
		t.Errorf("默认形状应为全圆")
	}
	if !strings.Contains(socialCSSFor(&Props{Shape: "rounded"}), "border-radius: 10px;") {
		t.Errorf("rounded 应为 10px")
	}
	if !strings.Contains(socialCSSFor(&Props{Shape: "square"}), "border-radius: 0;") {
		t.Errorf("square 应为 0")
	}
	sized := socialCSSFor(&Props{Size: "48px"})
	for _, want := range []string{"width: 48px;", "height: 48px;", "font-size: calc(48px * 0.55);"} {
		if !strings.Contains(sized, want) {
			t.Errorf("尺寸未贯穿到 %q\n%s", want, sized)
		}
	}
}

// TestSocialCSSHoverIsDesktopBucket 悬停抬升走桌面桶（不包 hover 媒体查询）。
//
// 这是迁移前就有的行为，与 productcard 相反；顺手钉住以免「顺手统一成 @hover」。
func TestSocialCSSHoverIsDesktopBucket(t *testing.T) {
	out := socialCSSFor(&Props{})
	if !strings.Contains(out, ".sky-c-t .sky-social-btn:hover {\n  transform: translateY(-2px);\n}") {
		t.Errorf("悬停规则形态变了:\n%s", out)
	}
	if strings.Contains(out, "(hover: hover)") {
		t.Errorf("悬停规则不该被包进 hover 媒体查询（会改变既有行为）:\n%s", out)
	}
}
