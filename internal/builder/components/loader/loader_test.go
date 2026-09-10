package loader

import (
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestValidateExtra 校验：形态白名单 + CSS 安全值。
func TestValidateExtra(t *testing.T) {
	cases := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空 props 合法（缺省 spinner）", &Props{}, false},
		{"形态合法（脉冲）", &Props{Variant: VariantPulse}, false},
		{"形态合法（波浪）", &Props{Variant: VariantWave}, false},
		{"形态合法（弹跳）", &Props{Variant: VariantBounce}, false},
		{"非法形态", &Props{Variant: "spiral"}, true},
		{"尺寸合法", &Props{Size: "48px"}, false},
		{"尺寸注入非法", &Props{Size: "48px;}"}, true},
		{"颜色注入非法", &Props{Color: "red;"}, true},
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

// TestCompileCSS 样式编译：缺省尺寸、四形态 keyframes、自定义尺寸。
func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{}, b)
	css := b.String()
	for _, want := range []string{
		"--wp-loader-size: 32px",
		".wp-c-n1 .wp-loader-ring",
		"animation: wp-loader-spin .8s linear infinite",
		"@keyframes wp-loader-spin",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}

	// 全量形态遍历（shapes 表即唯一清单，新增形态自动纳入）。
	for _, s := range shapes {
		b2 := &core.CSSBuckets{}
		compileCSS("n2", &Props{Variant: s.variant}, b2)
		if !strings.Contains(b2.String(), "@keyframes wp-loader-") {
			t.Errorf("形态 %s 缺少 keyframes", s.variant)
		}
	}

	b3 := &core.CSSBuckets{}
	compileCSS("n3", &Props{Size: "48px"}, b3)
	if !strings.Contains(b3.String(), "--wp-loader-size: 48px") {
		t.Errorf("自定义尺寸未生效:\n%s", b3.String())
	}
}

// TestBuildView 视图：形态 → DOM 结构映射 + 标签 + 确定性。
func TestBuildView(t *testing.T) {
	v := BuildView(&Props{Variant: VariantDots, Label: "加载中…"})
	if v.Self != "dot" || v.Wrap || len(v.Items) != 3 {
		t.Errorf("dots 视图不符: %+v", v)
	}
	if v.Label != "加载中…" {
		t.Errorf("标签不符: %q", v.Label)
	}
	// 容器形态：九宫格与波浪条的子元素都包在容器里。
	if g := BuildView(&Props{Variant: VariantGrid}); !g.Wrap || g.Self != "grid" || len(g.Items) != 9 {
		t.Errorf("grid 视图不符: %+v", g)
	}
	if w := BuildView(&Props{Variant: VariantWave}); !w.Wrap || w.Self != "wave" || len(w.Items) != 5 {
		t.Errorf("wave 视图不符: %+v", w)
	}
	// 单元素形态无子元素（plane/orbit 曾经被 default 吞成 spinner，这里锁死）。
	for _, variant := range []string{VariantPlane, VariantOrbit, VariantPulse, VariantSpinner} {
		if s := BuildView(&Props{Variant: variant}); s.Items != nil || s.Wrap {
			t.Errorf("形态 %s 应为单元素结构: %+v", variant, s)
		}
	}
	if p := BuildView(&Props{Variant: VariantBounce}); p.Self != "bounce" || len(p.Items) != 3 || p.Wrap {
		t.Errorf("bounce 视图不符: %+v", p)
	}
	p := &Props{Variant: VariantBars}
	if !reflect.DeepEqual(BuildView(p), BuildView(p)) {
		t.Errorf("BuildView 输出不确定")
	}
}

// TestShapeTableSync 形态表 → CSS 选择器必须一一对应：
// self 与 compileCSS 的后缀分叉，产物就会出现「模板输出的类名没有样式 / 样式指向不存在的元素」
// （plane/grid/orbit 曾经的缺陷）。本用例遍历 shapes 全表兜底。
func TestShapeTableSync(t *testing.T) {
	if len(shapes) != len(shapeByVariant) {
		t.Fatalf("形态表存在重复项：%d 项 / %d 唯一", len(shapes), len(shapeByVariant))
	}
	for _, s := range shapes {
		b := &core.CSSBuckets{}
		compileCSS("n1", &Props{Variant: s.variant}, b)
		css := b.String()
		sel := ".wp-c-n1 .wp-loader-" + s.self
		if !strings.Contains(css, sel) {
			t.Errorf("形态 %s：CSS 缺少选择器 %s", s.variant, sel)
		}
		if s.items > 0 && !strings.Contains(css, sel+" i") && !strings.Contains(css, sel+":nth-child") {
			t.Errorf("形态 %s：子元素缺少样式（选择器 %s）", s.variant, sel)
		}
	}
}
