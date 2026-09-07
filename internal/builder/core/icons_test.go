package core

import (
	"strings"
	"testing"
)

// contains 判断切片是否包含目标字符串。
func contains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

// TestFilterIconsByCategory 分类筛选：筛选结果必须全部属于该分类（不写死数量，
// 避免图标数量变化导致断言失效）。
func TestFilterIconsByCategory(t *testing.T) {
	for _, cat := range IconCategories() {
		got := FilterIcons(cat, "", "")
		if len(got) == 0 {
			t.Fatalf("分类 %q 应至少有一个图标", cat)
		}
		for _, name := range got {
			c, ok := IconCategory(name)
			if !ok {
				t.Fatalf("分类 %q 的筛选结果 %q 缺少元数据", cat, name)
			}
			if c != cat {
				t.Fatalf("分类 %q 的筛选结果 %q 实际属于分类 %q", cat, name, c)
			}
		}
	}
}

// TestFilterIconsByKeyword 搜索：匹配名称 / 中文标签 / 关键词（大小写不敏感）。
func TestFilterIconsByKeyword(t *testing.T) {
	// 中文标签搜索。
	if !contains(FilterIcons("", "", "邮件"), "mail") {
		t.Fatalf("关键词「邮件」应包含 mail，got %v", FilterIcons("", "", "邮件"))
	}
	// 关键词同义搜索（定位 → map-pin）。
	if !contains(FilterIcons("", "", "定位"), "map-pin") {
		t.Fatalf("关键词「定位」应包含 map-pin，got %v", FilterIcons("", "", "定位"))
	}
	// 名称搜索（大小写不敏感）。
	if !contains(FilterIcons("", "", "REFRESH"), "refresh") {
		t.Fatalf("关键词「REFRESH」应包含 refresh，got %v", FilterIcons("", "", "REFRESH"))
	}
	// 分类 + 关键词组合：结果仍须全部属于该分类。
	for _, name := range FilterIcons("media", "", "播放") {
		if c, _ := IconCategory(name); c != "media" {
			t.Fatalf("media+「播放」筛选结果 %q 不属于 media", name)
		}
	}
}

// TestFilterIconsEmpty 空过滤返回全部图标。
func TestFilterIconsEmpty(t *testing.T) {
	all := FilterIcons("", "", "")
	if len(all) != len(IconNames()) {
		t.Fatalf("空过滤应返回全部图标：got %d, want %d", len(all), len(IconNames()))
	}
}

// TestIconMeta 分类 / 标签元数据。
func TestIconMeta(t *testing.T) {
	if c, ok := IconCategory("truck"); !ok || c != "commerce" {
		t.Fatalf("truck 分类应为 commerce，got %q ok=%v", c, ok)
	}
	if IconLabel("shield") != "盾牌" {
		t.Fatalf("shield 标签应为「盾牌」，got %q", IconLabel("shield"))
	}
	if _, ok := IconCategory("not-exist"); ok {
		t.Fatal("未知图标分类应 ok=false")
	}
}

// TestIconCategories 分类数量与顺序（分类由 IconCategories 的 order 显式声明，
// 数量稳定，不随图标数量变化）。
func TestIconCategories(t *testing.T) {
	expect := []string{"arrows", "basic", "commerce", "communication", "navigation",
		"security", "social", "media", "editor", "weather", "files"}
	cats := IconCategories()
	if len(cats) != len(expect) {
		t.Fatalf("应返回 %d 个分类，got %d (%v)", len(expect), len(cats), cats)
	}
	for i := range expect {
		if cats[i] != expect[i] {
			t.Fatalf("分类顺序不匹配：位置 %d 期望 %q，got %q", i, expect[i], cats[i])
		}
	}
}

// TestIconCatalogComplete 每个内嵌 SVG 图标都应有分类元数据。
func TestIconCatalogComplete(t *testing.T) {
	names := IconNames()
	if len(names) < 100 {
		t.Fatalf("内置图标数量过少（应 ≥ 100），got %d", len(names))
	}
	for _, name := range names {
		if _, ok := IconCategory(name); !ok {
			t.Fatalf("图标 %q 缺少分类元数据", name)
		}
	}
}

// TestIconSVGFormat 图标 SVG 根开标签统一：描边 = fill=none + stroke-width=2，
// 实心（-fill 后缀）= fill=currentColor；去 width/height，不含 xmlns/class。
// 注意：仅校验根 <svg> 开标签，body 内 <rect>/<line> 等的 width/height 是合法图形属性。
func TestIconSVGFormat(t *testing.T) {
	const outlinedPrefix = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`
	const filledPrefix = `<svg viewBox="0 0 24 24" fill="currentColor">`
	for _, name := range IconNames() {
		svg, ok := IconSVG(name)
		if !ok || svg == "" {
			t.Fatalf("图标 %q SVG 为空", name)
		}
		wantPrefix := outlinedPrefix
		if strings.HasSuffix(name, "-fill") {
			wantPrefix = filledPrefix
		}
		if !strings.HasPrefix(svg, wantPrefix) {
			t.Fatalf("图标 %q 根开标签非规范格式", name)
		}
		if !strings.HasSuffix(svg, `</svg>`) {
			t.Fatalf("图标 %q 缺少 </svg> 闭合", name)
		}
		if strings.Contains(svg, `xmlns=`) || strings.Contains(svg, ` class=`) {
			t.Fatalf("图标 %q 含 xmlns/class 属性", name)
		}
	}
}

// TestIconStyle 风格元数据：实心（-fill 后缀）Style=filled，其余 outlined。
func TestIconStyle(t *testing.T) {
	for _, name := range IconNames() {
		style, ok := IconStyle(name)
		if !ok {
			t.Fatalf("图标 %q 缺少风格元数据", name)
		}
		if strings.HasSuffix(name, "-fill") {
			if style != "filled" {
				t.Fatalf("实心图标 %q Style 应为 filled，got %q", name, style)
			}
		} else if style != "outlined" {
			t.Fatalf("描边图标 %q Style 应为 outlined，got %q", name, style)
		}
	}
}

// TestFilterIconsByStyle 风格筛选：filled 只含实心图标，outlined 只含描边图标，
// 两者并集覆盖全部图标。
func TestFilterIconsByStyle(t *testing.T) {
	filled := FilterIcons("", "filled", "")
	if len(filled) == 0 {
		t.Fatal("filled 筛选应至少有一个图标")
	}
	for _, name := range filled {
		if !strings.HasSuffix(name, "-fill") {
			t.Fatalf("filled 筛选结果 %q 应为实心（-fill 后缀）", name)
		}
	}
	outlined := FilterIcons("", "outlined", "")
	for _, name := range outlined {
		if strings.HasSuffix(name, "-fill") {
			t.Fatalf("outlined 筛选结果 %q 不应为实心", name)
		}
	}
	if len(filled)+len(outlined) != len(IconNames()) {
		t.Fatalf("filled(%d)+outlined(%d) 应等于全部图标(%d)", len(filled), len(outlined), len(IconNames()))
	}
	// 组合：分类 + 风格。
	for _, name := range FilterIcons("social", "filled", "") {
		if c, _ := IconCategory(name); c != "social" {
			t.Fatalf("social+filled 结果 %q 不属于 social", name)
		}
		if !strings.HasSuffix(name, "-fill") {
			t.Fatalf("social+filled 结果 %q 应为实心", name)
		}
	}
	// star-fill 与 star 同属 social 分类，但风格不同。
	if c, ok := IconCategory("star-fill"); !ok || c != "social" {
		t.Fatalf("star-fill 分类应为 social，got %q ok=%v", c, ok)
	}
}
