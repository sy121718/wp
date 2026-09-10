package cardfan

import (
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 校验：尺寸为 CSS 安全值。
func TestValidateExtra(t *testing.T) {
	cases := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法（缺省 9 张）", &Props{}, false},
		{"尺寸合法", &Props{Width: "240px", Height: "320px"}, false},
		{"宽度注入非法", &Props{Width: "240px;}"}, true},
		{"高度注入非法", &Props{Height: "320px;}"}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "n1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

// TestCompileCSS 样式编译：容器堆叠 + 逐卡色相/悬停展开 + 按压置顶。
func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{}, b) // 默认 9 张
	css := b.String()

	// 容器：相对定位 + flex 居中。
	for _, want := range []string{"position: relative", "display: flex", "min-height: 320px"} {
		if !strings.Contains(css, want) {
			t.Errorf("容器缺少 %q\n%s", want, css)
		}
	}

	// 9 张卡都存在，且第 1/5/9 张色相分别为 -200/0/200（默认 hueStep=50）。
	for i, wantHue := range map[int]string{1: "-200deg", 5: "0deg", 9: "200deg"} {
		nth := "wp-cardfan-card:nth-child(" + strconv.Itoa(i) + ")"
		if !strings.Contains(css, nth) {
			t.Errorf("缺少第 %d 张卡选择器 %q", i, nth)
		}
		if !strings.Contains(css, "hue-rotate("+wantHue+")") {
			t.Errorf("第 %d 张卡色相应含 %s\n%s", i, wantHue, css)
		}
	}

	// 悬停展开：第 1 张 rotate(-20deg) translate(-480px)；第 9 张 +20/+480。
	for _, want := range []string{"rotate(-20deg) translate(-480px, -50px)", "rotate(20deg) translate(480px, -50px)"} {
		if !strings.Contains(css, want) {
			t.Errorf("悬停展开缺少 %q\n%s", want, css)
		}
	}

	// 悬停规则包触屏治理；按压规则不包。
	if !strings.Contains(css, "@media (hover: hover)") {
		t.Errorf("悬停展开应包 hover:hover\n%s", css)
	}
	if !strings.Contains(css, ":active .wp-cardfan-card") {
		t.Errorf("缺少容器按压态\n%s", css)
	}
	if !strings.Contains(css, "z-index: 100") {
		t.Errorf("缺少被点卡片置顶\n%s", css)
	}
}

// TestBuildView 渲染视图：卡片数量与数字 1~N。
func TestBuildView(t *testing.T) {
	v := BuildView(&Props{}) // 默认 9
	if len(v.Cards) != 9 {
		t.Fatalf("默认应 9 张卡，got %d", len(v.Cards))
	}
	for i, c := range v.Cards {
		if c.Label != strconv.Itoa(i+1) {
			t.Errorf("第 %d 张卡 label=%q, want %q", i, c.Label, strconv.Itoa(i+1))
		}
	}

	// 自定义数量 4。
	v4 := BuildView(&Props{Count: 4})
	if len(v4.Cards) != 4 {
		t.Fatalf("Count=4 应 4 张卡，got %d", len(v4.Cards))
	}
}
