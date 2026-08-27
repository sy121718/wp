package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/media"
)

// svgImageDoc 构造带 SVG 资产的文档。
func svgImageDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sv1","type":"core.image","props":`+props+`}]}`)
}

// TestImageSVGInline SVG 内联：识别 svg 类型，开启后源码内联输出。
func TestImageSVGInline(t *testing.T) {
	s := media.NewStore()
	id, _, err := s.Upload(media.Asset{
		Hash: hash64("svg"), FileName: "logo.svg", MimeType: "image/svg+xml",
		Type: media.TypeSVG, Width: 100, Height: 40,
		Variants: []media.Variant{{Kind: media.VariantOriginal, Format: "svg", URL: "<svg xmlns=\"http://www.w3.org/2000/svg\"><path d=\"M0 0\" fill=\"currentColor\"/></svg>", Width: 100, Height: 40}},
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	c, err := builder.Compile(svgImageDoc(t, `{"assetId":"`+id+`","inlineSvg":true}`), builder.WithMediaResolver(s))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "<svg") || strings.Contains(c.HTML, "<img") {
		t.Errorf("SVG 内联输出异常: %s", c.HTML)
	}
	// 关闭开关：标准 img。
	c2, err := builder.Compile(svgImageDoc(t, `{"assetId":"`+id+`"}`), builder.WithMediaResolver(s))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c2.HTML, "<img") {
		t.Errorf("默认应输出 img: %s", c2.HTML)
	}
}

// TestImagePhotoCompile 照片全流程：比例/适应/对齐/宽度/滤镜/悬浮/懒加载/图注。
func TestImagePhotoCompile(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("photo2"), "产品图")

	c, err := builder.Compile(svgImageDoc(t, `{
		"assetId":"`+id+`",
		"aspectRatio":"16:9",
		"objectFit":"cover",
		"align":{"desktop":"center","mobile":"left"},
		"width":"60%",
		"maxWidth":"480px",
		"filters":{"grayscale":100},
		"hover":{"scale":"1.05","restoreColor":true,"duration":"300ms"},
		"loading":"eager",
		"fetchPriority":"high",
		"caption":"图 1.1 系统管线",
		"sizes":"(max-width: 768px) 100vw, 600px",
		"advanced":{"margin":{"desktop":{"bottom":"24px"}}}
	}`), builder.WithMediaResolver(s))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		"<figure>", "<figcaption>图 1.1 系统管线</figcaption>",
		"loading=\"eager\"", "fetchpriority=\"high\"", "decoding=\"async\"",
		"sizes=\"(max-width: 768px) 100vw, 600px\"",
		"aspect-ratio: 16 / 9",
		"width: 60%", "max-width: 480px",
		"filter: grayscale(100%)",
		"transition: transform 300ms ease, filter 300ms ease",
		".wp-c-sv1:hover", "scale(1.05)",
		"margin-bottom: 24px", // Advanced 由基座编译
	} {
		if !strings.Contains(c.CSS, want) && !strings.Contains(c.HTML, want) {
			t.Errorf("输出缺少 %q\nHTML: %s\nCSS: %s", want, c.HTML, c.CSS)
		}
	}
}

// TestImageExternalURL 外部 URL 源：直接使用，宽高缺省不报错。
func TestImageExternalURL(t *testing.T) {
	c, err := builder.Compile(svgImageDoc(t, `{"src":"https://cdn.example.com/hero.jpg","width":"100%","alt":"外链图"}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `src="https://cdn.example.com/hero.jpg"`) {
		t.Errorf("外部 URL 未生效: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, `alt="外链图"`) {
		t.Errorf("alt 缺失: %s", c.HTML)
	}
}

// TestImageLightbox 零 JS 灯箱：拉链式 :target 浮层。
func TestImageLightbox(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("lb"), "灯箱图")
	c, err := builder.Compile(svgImageDoc(t, `{"assetId":"`+id+`","clickAction":"lightbox"}`), builder.WithMediaResolver(s))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		`<a href="#wp-lb-sv1">`,
		`id="wp-lb-sv1" class="wp-lightbox"`,
		".wp-lightbox:target",
		"display: flex",
	} {
		if !strings.Contains(c.HTML, want) && !strings.Contains(c.CSS, want) {
			t.Errorf("灯箱输出缺少 %q", want)
		}
	}
}

// TestImageCMSBinding CMS 图片字段绑定：解析 assetId；空值回退占位。
func TestImageCMSBinding(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("bind"), "绑定图")
	resolver := mapResolver{"post.featuredImage": id}
	c, err := builder.Compile(svgImageDoc(t, `{"binding":{"field":"post.featuredImage","fallback":"ast_placeholder"}}`),
		builder.WithMediaResolver(s), builder.WithContentResolver(resolver))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "hero.jpg") || !strings.Contains(c.HTML, "<img") {
		t.Errorf("绑定解析异常: %s", c.HTML)
	}
}

// TestImageValidate 规范级校验拒绝。
func TestImageValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"无媒体源", `{}`, "必须提供"},
		{"双源冲突", `{"assetId":"ast_x","src":"https://a.com/x.jpg"}`, "只能二选一"},
		{"自定义比例缺值", `{"src":"https://a.com/x.jpg","aspectRatio":"custom"}`, "必须提供 w / h"},
		{"非法对齐", `{"src":"https://a.com/x.jpg","align":{"desktop":"middle"}}`, "无效的 desktop 端对齐"},
		{"滤镜越界", `{"src":"https://a.com/x.jpg","filters":{"contrast":150}}`, "对比度必须在 1~100"},
		{"非法绑定路径", `{"binding":{"field":"Post.x"}}`, "无效的绑定字段路径"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(svgImageDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}