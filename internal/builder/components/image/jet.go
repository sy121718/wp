// Package image — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：图片 URL 直出 + 点击动作（链接/灯箱）+ 图注包裹
// 保留在 Go（预计算完整 HTML 片段），image.jet 模板仅原样输出。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package image

import (
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出图片样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View image 渲染视图数据（供 image.jet 模板使用）。
type View struct {
	// HTML 完整图片/灯箱/图注 HTML 片段（已转义，原样输出）。
	HTML string
}

// BuildView 生成图片渲染 HTML：URL 直出 + 点击动作 + 图注（与 render 输出结构一致）。
// class 为已合并的节点 class（nodeView 层计算），customID 为 Advanced 自定义 Element ID。
func BuildView(node *core.Node, p *Props, class, customID string, content core.ContentResolver) (View, error) {
	// 图片地址：CMS 绑定优先，否则手填 Src（媒体库/外链统一 URL）。
	src := p.Src
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			src = v
		} else if p.Binding.Fallback != "" {
			src = p.Binding.Fallback
		}
	}
	if src == "" {
		return View{}, fmt.Errorf("图片地址为空")
	}
	// 协议白名单校验：拒绝 javascript:/data:/vbscript: 等危险协议（降级为空 src，不阻断编译）。
	if !core.IsSafeURL(src) {
		src = ""
	}

	// 组装 <img>：URL 直出，宽高由 CSS 控制，无媒体库变体解析。
	var sb strings.Builder
	sb.WriteString(`<img src="`)
	sb.WriteString(html.EscapeString(src))
	sb.WriteString(`"`)
	if class != "" {
		sb.WriteString(` class="`)
		sb.WriteString(html.EscapeString(class))
		sb.WriteString(`"`)
	}
	if p.Loading == "eager" {
		sb.WriteString(` loading="eager"`)
	} else {
		sb.WriteString(` loading="lazy"`)
	}
	if p.FetchPriority == "high" {
		sb.WriteString(` fetchpriority="high"`)
	}
	sb.WriteString(` decoding="async" alt="`)
	sb.WriteString(html.EscapeString(p.Alt))
	sb.WriteString(`"`)
	if p.Title != "" {
		sb.WriteString(` title="`)
		sb.WriteString(html.EscapeString(p.Title))
		sb.WriteString(`"`)
	}
	sb.WriteString(`>`)
	imgHTML := sb.String()

	// 点击动作：lightbox 零 JS 实现（CSS :target 浮层）。
	if p.ClickAction == "lightbox" {
		imgHTML = `<a href="#wp-lb-` + node.ID + `">` + imgHTML + `</a>` +
			lightboxHTML(node.ID, src)
	} else if p.Link != "" || p.ClickAction == "link" {
		linkAttrs := `href="` + html.EscapeString(p.Link) + `"`
		if p.LinkTarget == "blank" {
			linkAttrs += ` target="_blank"`
		}
		if p.LinkRel == "nofollow" {
			linkAttrs += ` rel="nofollow"`
		}
		if customID != "" {
			linkAttrs += ` id="` + customID + `"`
		}
		imgHTML = `<a ` + linkAttrs + `>` + imgHTML + `</a>`
	} else if customID != "" {
		imgHTML = strings.Replace(imgHTML, "<img ", "<img id=\""+customID+"\" ", 1)
	}

	// 图注包裹。
	if p.Caption != "" {
		tmp := imgHTML
		imgHTML = `<figure>` + tmp + `<figcaption>` + html.EscapeString(p.Caption) + `</figcaption></figure>`
	}

	return View{HTML: imgHTML}, nil
}
