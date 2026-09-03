package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// svgImageDoc 构造带图片的文档。
func svgImageDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"sv1","type":"core.image","props":`+props+`}]}`)
}

// TestImagePhotoCompile 照片全流程：比例/适应/对齐/宽度/滤镜/悬浮/懒加载/图注。
func TestImagePhotoCompile(t *testing.T) {
	c, err := compile(t, svgImageDoc(t, `{
		"src":"/storage/hero.jpg",
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
		"advanced":{"margin":{"desktop":{"bottom":"24px"}}}
	}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		"<figure>", "<figcaption>图 1.1 系统管线</figcaption>",
		"loading=\"eager\"", "fetchpriority=\"high\"", "decoding=\"async\"",
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
	c, err := compile(t, svgImageDoc(t, `{"src":"https://cdn.example.com/hero.jpg","width":"100%","alt":"外链图"}`))
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
	c, err := compile(t, svgImageDoc(t, `{"src":"/storage/lightbox.jpg","clickAction":"lightbox"}`))
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

// TestImageCMSBinding CMS 图片字段绑定：解析 URL；空值回退 fallback（URL）。
func TestImageCMSBinding(t *testing.T) {
	resolver := mapResolver{"post.featuredImage": "/storage/bind.jpg"}
	c, err := compile(t, svgImageDoc(t, `{"binding":{"field":"post.featuredImage","fallback":"/storage/fallback.jpg"}}`),
		builder.WithContentResolver(resolver))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "/storage/bind.jpg") || !strings.Contains(c.HTML, "<img") {
		t.Errorf("绑定解析异常: %s", c.HTML)
	}
}

// TestImageValidate 规范级校验拒绝。
func TestImageValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"无媒体源", `{}`, "必须提供"},
		{"自定义比例缺值", `{"src":"https://a.com/x.jpg","aspectRatio":"custom"}`, "必须提供 w / h"},
		{"非法对齐", `{"src":"https://a.com/x.jpg","align":{"desktop":"middle"}}`, "无效的 desktop 端对齐"},
		{"滤镜越界", `{"src":"https://a.com/x.jpg","filters":{"contrast":150}}`, "对比度必须在 1~100"},
		{"非法绑定路径", `{"binding":{"field":"Post.x"}}`, "无效的绑定字段路径"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, svgImageDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}