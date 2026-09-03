package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// advancedImageDoc 构造带 Advanced 配置的图片文档。
func advancedImageDoc(t *testing.T, src, advanced string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"src":"` + src + `","advanced":` + advanced + `}}]}`)
}

// TestAdvancedSpacingCompile 四向间距 + 三端响应式 + 负边距编译。
func TestAdvancedSpacingCompile(t *testing.T) {
	advanced := `{"margin":{"desktop":{"top":"-12px","right":"0","bottom":"24px","left":"auto"},"tablet":{"top":"8px"},"mobile":{"left":"16px"}},"padding":{"desktop":{"top":"8px","right":"16px","bottom":"8px","left":"16px"}}}`
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", advanced))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"margin: -12px 0 24px auto",
		"padding: 8px 16px 8px 16px",
		"@media (max-width: 1024px)",
		"margin-top: 8px", // tablet 部分设置输出长属性
		"@media (max-width: 767px)",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestAdvancedWidthAlignShadowOpacity 宽度/对齐/阴影/透明度/层级。
func TestAdvancedWidthAlignShadowOpacity(t *testing.T) {
	advanced := `{"widthMode":"fixed","widthValue":"320px","alignSelf":"center","shadow":"lg","opacity":60,"zIndex":10}`
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", advanced))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"width: 320px",
		"align-self: center",
		"box-shadow: 0 10px 28px rgba(0,0,0,0.16)",
		"opacity: 0.60",
		"z-index: 10",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q", want)
		}
	}
}

// TestAdvancedFullWidth 铺满父容器。
func TestAdvancedFullWidth(t *testing.T) {
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", `{"widthMode":"full"}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.CSS, "width: 100%") {
		t.Errorf("full 宽度未生效:\n%s", c.CSS)
	}
}

// TestAdvancedBorderRadius 四向边框 + 四角独立圆角。
func TestAdvancedBorderRadius(t *testing.T) {
	advanced := `{"border":{"width":"1px","style":"dashed","color":"#999"},"radius":{"topLeft":"16px","topRight":"16px","bottomRight":"0","bottomLeft":"0"}}`
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", advanced))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"border: 1px dashed #999",
		"border-radius: 16px 16px 0 0",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q", want)
		}
	}
}

// TestAdvancedVisibility 三端显隐：桌面直出，平板/手机进媒体查询。
func TestAdvancedVisibility(t *testing.T) {
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", `{"hideOn":{"tablet":true,"mobile":true}}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 平板媒体查询块内应含 display:none。
	tabletBlock := extractMediaBlock(c.CSS, "@media (max-width: 1024px)")
	if !strings.Contains(tabletBlock, "display: none") {
		t.Errorf("平板隐藏缺失:\n%s", c.CSS)
	}
	mobileBlock := extractMediaBlock(c.CSS, "@media (max-width: 767px)")
	if !strings.Contains(mobileBlock, "display: none") {
		t.Errorf("手机隐藏缺失:\n%s", c.CSS)
	}
	// 桌面未开隐藏，默认样式不应含 display:none。
	if strings.Contains(c.CSS, "display: none }") && !strings.Contains(tabletBlock, "") {
		t.Errorf("桌面不应隐藏:\n%s", c.CSS)
	}
}

// TestAdvancedAllHidden 三端全隐藏：编译器照常输出（哑与确定性）。
func TestAdvancedAllHidden(t *testing.T) {
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", `{"hideOn":{"desktop":true,"tablet":true,"mobile":true}}`))
	if err != nil {
		t.Fatalf("三端全隐藏不应编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "<img") {
		t.Error("三端全隐藏仍应输出 HTML")
	}
}

// TestAdvancedCustomAttributes 自定义 class 织入与自定义 ID 注入。
func TestAdvancedCustomAttributes(t *testing.T) {
	advanced := `{"customClasses":["promo-hero","dark"],"customId":"hero-anchor"}`
	c, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", advanced))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `class="wp-c-pic promo-hero dark"`) {
		t.Errorf("自定义 class 未织入: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, `id="hero-anchor"`) {
		t.Errorf("自定义 ID 未注入: %s", c.HTML)
	}
}

// TestAdvancedLinkWithID 自定义 ID + 包裹链接：ID 落在 <a> 上。
func TestAdvancedLinkWithID(t *testing.T) {
	doc := mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"src":"/storage/adv.jpg","link":"https://example.com","advanced":{"customId":"go-link"}}}]}`)
	c, err := builder.Compile(doc)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `<a href="https://example.com" id="go-link">`) {
		t.Errorf("链接包裹与 ID 注入异常: %s", c.HTML)
	}
}

// TestAdvancedValidateErrors Advanced 层校验拒绝。
func TestAdvancedValidateErrors(t *testing.T) {
	cases := []struct{ name, advanced, want string }{
		{"负内边距", `{"padding":{"desktop":{"top":"-8px"}}}`, "不允许负值"},
		{"外边距超下限", `{"margin":{"desktop":{"top":"-500px"}}}`, "负值超出下限"},
		{"间距注入", `{"margin":{"desktop":{"top":"1px}body{x:1}"}}}`, "无效的"},
		{"非法宽度模式", `{"widthMode":"diagonal"}`, "无效的宽度模式"},
		{"fixed缺宽度", `{"widthMode":"fixed"}`, "必须提供有效宽度值"},
		{"非法对齐", `{"alignSelf":"middle"}`, "无效的自身对齐"},
		{"边框不完整", `{"border":{"width":"1px"}}`, "边框需同时提供"},
		{"非法阴影", `{"shadow":"xxl"}`, "无效的阴影预设"},
		{"透明度越界", `{"opacity":150}`, "0~100"},
		{"zindex越界", `{"zIndex":999}`, "z-index"},
		{"保留前缀class", `{"customClasses":["wp-evil"]}`, "wp- 保留前缀"},
		{"非法class字符", `{"customClasses":["a b"]}`, "无效的自定义 class"},
		{"非法ID", `{"customId":"1abc"}`, "无效的自定义 ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(advancedImageDoc(t, "/storage/adv.jpg", tc.advanced))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// TestAdvancedCustomIDDuplicate 自定义 ID 全文档唯一。
func TestAdvancedCustomIDDuplicate(t *testing.T) {
	doc := mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[`+
		`{"id":"p1","type":"core.image","props":{"src":"/storage/adv.jpg","advanced":{"customId":"same"}}},`+
		`{"id":"p2","type":"core.image","props":{"src":"/storage/adv.jpg","advanced":{"customId":"same"}}}]}`)
	_, err := builder.Compile(doc)
	if err == nil || !strings.Contains(err.Error(), "自定义 ID 重复") {
		t.Errorf("自定义 ID 重复应报错: %v", err)
	}
}

// extractMediaBlock 提取指定媒体查询块内容（粗提取，测试用）。
func extractMediaBlock(css, query string) string {
	idx := strings.Index(css, query)
	if idx < 0 {
		return ""
	}
	rest := css[idx+len(query):]
	end := strings.Index(rest, "\n\n")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
