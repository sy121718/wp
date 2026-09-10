package shapedivider

import (
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 校验：形状白名单、多层/漂移组合约束、CSS 安全值。
func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法（缺省 wave 单层）", &Props{}, false},
		{"六种形状合法", &Props{Shape: ShapeRound}, false},
		{"非法形状", &Props{Shape: "zigzag"}, true},
		{"层数越界上", &Props{Layers: 4}, true},
		{"层数越界下", &Props{Layers: -1}, true},
		{"wave 多层合法", &Props{Shape: ShapeWave, Layers: 3}, false},
		{"curve 多层合法", &Props{Shape: ShapeCurve, Layers: 2}, false},
		{"slope 多层非法", &Props{Shape: ShapeSlope, Layers: 2}, true},
		{"triangle 多层非法", &Props{Shape: ShapeTriangle, Layers: 3}, true},
		{"round 多层非法", &Props{Shape: ShapeRound, Layers: 2}, true},
		{"wave+drift+多层合法", &Props{Shape: ShapeWave, Layers: 2, Animate: AnimDrift}, false},
		{"wave+drift+单层非法", &Props{Animate: AnimDrift}, true},
		{"curve+drift 非法（漂移仅 wave）", &Props{Shape: ShapeCurve, Layers: 2, Animate: AnimDrift}, true},
		{"slope+drift 非法", &Props{Shape: ShapeSlope, Animate: AnimDrift}, true},
		{"非法动画取值", &Props{Animate: "bounce"}, true},
		{"高度合法", &Props{Height: Height{Desktop: "160px"}}, false},
		{"高度注入非法", &Props{Height: Height{Desktop: "120px;position:fixed"}}, true},
		{"颜色合法", &Props{Color: "#0a84ff"}, false},
		{"前景色注入非法", &Props{Color: "red;}"}, true},
		{"背景层色注入非法", &Props{ColorBack: "url(x)"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "n1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

// TestBuildView 层视图：缺省单层、多层景深、漂移 class、确定性。
func TestBuildView(t *testing.T) {
	t.Run("缺省单层 currentColor", func(t *testing.T) {
		v := BuildView(&Props{})
		if len(v.Layers) != 1 {
			t.Fatalf("缺省应 1 层，got %d", len(v.Layers))
		}
		l := v.Layers[0]
		if l.Fill != "currentColor" || l.FillOpacity != "" || l.Class != "" {
			t.Errorf("前景层字段不符: %+v", l)
		}
		if !strings.HasPrefix(l.D, "M0 16 c130 0 230 88 360 88") {
			t.Errorf("缺省波形 path 不符: %q", l.D)
		}
	})
	t.Run("wave 三层景深", func(t *testing.T) {
		v := BuildView(&Props{Layers: 3})
		if len(v.Layers) != 3 {
			t.Fatalf("应 3 层，got %d", len(v.Layers))
		}
		if v.Layers[1].FillOpacity != "0.55" || v.Layers[2].FillOpacity != "0.3" {
			t.Errorf("背景层透明度不符: %+v", v.Layers)
		}
		// 三层 path 互不相同（振幅变体）。
		if v.Layers[0].D == v.Layers[1].D || v.Layers[1].D == v.Layers[2].D {
			t.Errorf("层 path 应互不相同")
		}
	})
	t.Run("drift 仅背景层带动画 class", func(t *testing.T) {
		v := BuildView(&Props{Layers: 3, Animate: AnimDrift})
		if v.Layers[0].Class != "" {
			t.Errorf("前景层不应有动画 class: %q", v.Layers[0].Class)
		}
		if v.Layers[1].Class != "sd-l2" || v.Layers[2].Class != "sd-l3" {
			t.Errorf("动画层 class 不符: %q %q", v.Layers[1].Class, v.Layers[2].Class)
		}
		// 漂移扩宽：起点 x=-720。
		if !strings.HasPrefix(v.Layers[1].D, "M-720 ") {
			t.Errorf("漂移波形应外扩起点: %q", v.Layers[1].D)
		}
	})
	t.Run("ColorBack 显式色不带透明度", func(t *testing.T) {
		v := BuildView(&Props{Layers: 2, Color: "#111", ColorBack: "#eee"})
		if v.Layers[1].Fill != "#eee" || v.Layers[1].FillOpacity != "" {
			t.Errorf("显式背景层色不符: %+v", v.Layers[1])
		}
	})
	t.Run("curve 多层变体", func(t *testing.T) {
		v := BuildView(&Props{Shape: ShapeCurve, Layers: 2})
		if !strings.HasPrefix(v.Layers[1].D, "M0 120C280 44") {
			t.Errorf("curve 中层变体不符: %q", v.Layers[1].D)
		}
	})
	t.Run("确定性：同 props 两次输出 deep equal", func(t *testing.T) {
		p := &Props{Shape: ShapeWave, Layers: 3, Animate: AnimDrift, Color: "#123456", FlipX: true}
		if !reflect.DeepEqual(BuildView(p), BuildView(p)) {
			t.Errorf("BuildView 输出不确定")
		}
	})
}

// TestCompileCSS 样式编译：三端高度缺省/覆盖、镜像、漂移动画。
func TestCompileCSS(t *testing.T) {
	tests := []struct {
		name  string
		props *Props
		wants []string
		not   []string
	}{
		{
			name:  "三端高度缺省",
			props: &Props{},
			wants: []string{"height: 120px", "height: 90px", "height: 64px"},
		},
		{
			name:  "自定义高度覆盖",
			props: &Props{Height: Height{Desktop: "200px", Mobile: "40px"}},
			wants: []string{"height: 200px", "height: 40px"},
			not:   []string{"height: 120px"},
		},
		{
			name:  "水平镜像",
			props: &Props{FlipX: true},
			wants: []string{"transform: scaleX(-1)"},
		},
		{
			name:  "双向镜像",
			props: &Props{FlipX: true, FlipY: true},
			wants: []string{"transform: scaleX(-1) scaleY(-1)"},
		},
		{
			name:  "drift 动画与关键帧",
			props: &Props{Layers: 2, Animate: AnimDrift},
			wants: []string{"animation: wp-sd-drift 14s ease-in-out infinite alternate", "@keyframes wp-sd-drift"},
		},
		{
			name:  "无 drift 无动画",
			props: &Props{},
			wants: []string{"position: relative"},
			not:   []string{"animation:", "@keyframes wp-sd-drift"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &core.CSSBuckets{}
			compileCSS("n1", tt.props, b)
			css := b.String()
			for _, want := range tt.wants {
				if !strings.Contains(css, want) {
					t.Errorf("CSS 缺少 %q\n%s", want, css)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(css, n) {
					t.Errorf("CSS 不应包含 %q\n%s", n, css)
				}
			}
		})
	}
}

// TestWavePathDeterminism 波形生成确定性：同参数恒同输出（整数运算无浮点漂移）。
// 生成实现已提升至 core 通用素材库（core.ShapePath），此处经组件委托入口验证。
func TestWavePathDeterminism(t *testing.T) {
	for _, variant := range []int{0, 1, 2} {
		a := shapePath(ShapeWave, variant, false)
		b := shapePath(ShapeWave, variant, false)
		if a != b {
			t.Errorf("变体 %d 波形不确定", variant)
		}
	}
}
