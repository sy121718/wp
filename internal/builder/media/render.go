package media

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// ImageHTMLOptions 图片 HTML 输出选项。
type ImageHTMLOptions struct {
	// Class 附加 class（如节点类名 wp-c-<id>）。
	Class string
	// Sizes srcset 的 sizes 提示，如 "(max-width: 767px) 100vw, 50vw"。
	Sizes string
	// Lazy 懒加载，默认开启（输出 loading="lazy"）。
	Lazy bool
	// lazySet 显式标记 Lazy 是否被设置（零值默认开启）。
	lazySet bool
	// Loading 显式加载策略：lazy / eager（空 = 默认 lazy）。
	Loading string
	// FetchPriority 加载优先级：high / auto（空 = 不输出）。
	FetchPriority string
}

// WithLazy 设置懒加载开关。
func (o *ImageHTMLOptions) WithLazy(v bool) *ImageHTMLOptions {
	o.Lazy = v
	o.lazySet = true
	return o
}

// lazyValue 解析懒加载最终取值（显式 Loading 优先，其次默认开启）。
func (o ImageHTMLOptions) lazyValue() bool {
	if o.Loading == "eager" {
		return false
	}
	if o.Loading != "" {
		return true
	}
	if o.lazySet {
		return o.Lazy
	}
	return true
}

// RenderImageHTML 把图片类媒体元数据编译为标准 HTML（规范 §4 数据流终点）：
//
//	<img loading="lazy" width="..." height="..." src="..." srcset="..." alt="...">
//
// 存在现代格式变体时输出 <picture>（AVIF → WebP → 基础格式 fallback）。
// alt/title 支持组件局部覆盖（默认继承全局 SEO 元数据）。
// 仅接受 image / svg 类型，其他媒体类型返回错误（视频/文档由各自渲染器处理）。
func RenderImageHTML(meta *core.MediaMeta, altOverride, titleOverride string, opts ImageHTMLOptions) (out string, err error) {
	if meta.Type != core.MediaTypeImage && meta.Type != core.MediaTypeSVG {
		return "", fmt.Errorf("RenderImageHTML 仅接受图片类媒体，实际类型: %s", meta.Type)
	}
	alt := meta.Alt
	if altOverride != "" {
		alt = altOverride
	}
	title := meta.Title
	if titleOverride != "" {
		title = titleOverride
	}

	img := renderImgTag(meta, alt, title, opts)
	if len(meta.Sources) == 0 {
		return img, nil
	}

	var sb strings.Builder
	sb.WriteString("<picture>")
	// AVIF / WebP 顺序由解析器保证（现代格式优先声明）。
	for _, src := range meta.Sources {
		sb.WriteString(`<source type="`)
		sb.WriteString(html.EscapeString(src.Type))
		sb.WriteString(`" srcset="`)
		sb.WriteString(html.EscapeString(src.Srcset))
		sb.WriteString(`"`)
		if opts.Sizes != "" {
			sb.WriteString(` sizes="`)
			sb.WriteString(html.EscapeString(opts.Sizes))
			sb.WriteString(`"`)
		}
		sb.WriteString(">")
	}
	sb.WriteString(img)
	sb.WriteString("</picture>")
	return sb.String(), nil
}

// renderImgTag 渲染 <img> 标签：宽高必输出（杜绝 CLS），属性值全部转义。
func renderImgTag(meta *core.MediaMeta, alt, title string, opts ImageHTMLOptions) string {
	var sb strings.Builder
	sb.WriteString("<img src=\"")
	sb.WriteString(html.EscapeString(meta.URL))
	sb.WriteString("\"")
	if opts.Class != "" {
		sb.WriteString(" class=\"")
		sb.WriteString(html.EscapeString(opts.Class))
		sb.WriteString("\"")
	}
	// 宽高必写：浏览器预留排版空间，杜绝前端排版抖动。
	sb.WriteString(" width=\"")
	sb.WriteString(strconv.Itoa(meta.Width))
	sb.WriteString("\" height=\"")
	sb.WriteString(strconv.Itoa(meta.Height))
	sb.WriteString("\"")
	if opts.lazyValue() {
		sb.WriteString(" loading=\"lazy\"")
	} else if opts.Loading == "eager" {
		sb.WriteString(" loading=\"eager\"")
	}
	if opts.FetchPriority == "high" {
		sb.WriteString(" fetchpriority=\"high\"")
	}
	sb.WriteString(" decoding=\"async\"")
	if meta.Srcset != "" {
		sb.WriteString(" srcset=\"")
		sb.WriteString(html.EscapeString(meta.Srcset))
		sb.WriteString("\"")
		if opts.Sizes != "" {
			sb.WriteString(" sizes=\"")
			sb.WriteString(html.EscapeString(opts.Sizes))
			sb.WriteString("\"")
		}
	}
	sb.WriteString(" alt=\"")
	sb.WriteString(html.EscapeString(alt))
	sb.WriteString("\"")
	if title != "" {
		sb.WriteString(" title=\"")
		sb.WriteString(html.EscapeString(title))
		sb.WriteString("\"")
	}
	sb.WriteString(">")
	return sb.String()
}