package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// galleryDoc 构造 gallery 节点文档。
func galleryDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g1","type":"core.gallery","props":`+props+`}]}`)
}

// TestGalleryGrid 网格模式：纯 CSS Grid 三端列数 + 单图项输出 + 统一样式。
func TestGalleryGrid(t *testing.T) {
	items := `[{"url":"/storage/1.jpg","alt":"角图A","caption":"细节A"},{"url":"/storage/2.jpg"},{"url":"/storage/3.jpg"}]`
	props := `{"mode":"grid","items":` + items + `,"grid":{"columns":{"desktop":3,"tablet":2,"mobile":1},"columnGap":"12px","rowGap":"8px"},"aspectRatio":"1:1","objectFit":"cover","radius":"8px","captionMode":"below"}`

	c, err := compile(t, galleryDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	// HTML：三张图 img + 角图 A 的 alt 覆盖 + 图注。
	if strings.Count(c.HTML, "<img") != 3 {
		t.Errorf("图片数量异常: %d\n%s", strings.Count(c.HTML, "<img"), c.HTML)
	}
	if !strings.Contains(c.HTML, `alt="角图A"`) {
		t.Errorf("单图 alt 覆盖缺失: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, "<figcaption>细节A</figcaption>") {
		t.Errorf("图注缺失: %s", c.HTML)
	}

	// CSS：三端列数 + 间距 + 统一比例/圆角。
	for _, want := range []string{
		"grid-template-columns: repeat(3, 1fr)",
		"@media (max-width: 1024px)",
		"repeat(2, 1fr)",
		"@media (max-width: 767px)",
		"repeat(1, 1fr)",
		"column-gap: 12px", "row-gap: 8px",
		"aspect-ratio: 1 / 1", "border-radius: 8px",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestGalleryCarousel 轮播骨架：data-carousel 增强属性 + 轨道/滑动结构 + 导航控件。
func TestGalleryCarousel(t *testing.T) {
	items := `[{"url":"/storage/1.jpg"},{"url":"/storage/2.jpg"},{"url":"/storage/3.jpg"}]`
	props := `{"mode":"carousel","items":` + items + `,"carousel":{"autoplay":true,"interval":3000,"infinite":true,"pauseOnHover":true,"slidesPerView":{"desktop":2.5,"tablet":2,"mobile":1},"arrows":true,"dots":true}}`

	c, err := compile(t, galleryDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		`class="gallery-carousel"`,
		`data-carousel='{"autoplay":true,"interval":3000,"infinite":true,"pauseOnHover":true,"slidesPerView":{"desktop":2.5,"tablet":2,"mobile":1}}'`,
		`class="gallery-track"`,
		`class="gallery-slide"`,
		`gallery-prev`, `gallery-next`, `gallery-dots`,
		"scroll-snap-type: x mandatory",
	} {
		if !strings.Contains(c.HTML, want) && !strings.Contains(c.CSS, want) {
			t.Errorf("输出缺少 %q", want)
		}
	}
}

// TestGalleryBinding CMS 图集绑定：字符串数组解析；空值占位兜底；空值无占位隐藏。
func TestGalleryBinding(t *testing.T) {
	bindJSON := `["/storage/1.jpg","/storage/2.jpg"]`
	c, err := compile(t, galleryDoc(t, `{"binding":{"field":"product.gallery"}}`),
		builder.WithContentResolver(mapResolver{"product.gallery": bindJSON}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Count(c.HTML, "<img") != 2 {
		t.Errorf("绑定图集张数异常: %d\n%s", strings.Count(c.HTML, "<img"), c.HTML)
	}

	// 空值 + 占位图：输出占位。
	c2, err := compile(t, galleryDoc(t, `{"binding":{"field":"product.gallery","placeholder":"/storage/3.jpg"}}`),
		builder.WithContentResolver(mapResolver{"product.gallery": ""}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Count(c2.HTML, "<img") != 1 {
		t.Errorf("占位图输出异常: %s", c2.HTML)
	}

	// 空值 + 无占位：组件隐藏（空 HTML）。
	c3, err := compile(t, galleryDoc(t, `{"binding":{"field":"product.gallery"}}`),
		builder.WithContentResolver(mapResolver{"product.gallery": ""}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c3.HTML, "<img") {
		t.Errorf("空图集应隐藏组件: %s", c3.HTML)
	}
}

// TestGalleryHover 悬浮反馈：缩放/遮罩/阴影加深。
func TestGalleryHover(t *testing.T) {
	c, err := compile(t, galleryDoc(t, `{"items":[{"url":"/storage/1.jpg"}],"hover":{"scale":"1.05","overlay":"dark","deepen":true,"duration":"400ms"}}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"transform: scale(1.05)",
		"box-shadow: 0 10px 28px rgba(0,0,0,0.16)",
		"rgba(0,0,0,0.35)",
		"transition: transform 400ms ease",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestGalleryValidate 校验拒绝。
func TestGalleryValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"无数据源", `{}`, "必须提供"},
		{"非法列数", `{"items":[{"url":"/storage/1.jpg"}],"grid":{"columns":{"desktop":9}}}`, "1~8"},
		{"非法绑定路径", `{"binding":{"field":"Product.x"}}`, "无效的绑定字段路径"},
		{"link动作缺链接", `{"items":[{"url":"/storage/1.jpg"}],"clickAction":"link"}`, "必须提供默认链接"},
		{"占位图非法", `{"binding":{"field":"product.gallery","placeholder":"bad url with space"}}`, "无效的占位图 URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, galleryDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// TestGalleryDeterministic 确定性构建。
func TestGalleryDeterministic(t *testing.T) {
	props := `{"items":[{"url":"/storage/det.jpg"}],"mode":"carousel","carousel":{"autoplay":true}}`
	c1, err := compile(t, galleryDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	c2, err := compile(t, galleryDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if c1.HTML != c2.HTML || c1.CSS != c2.CSS {
		t.Error("同一文档两次编译输出不一致")
	}
}
