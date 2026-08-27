// Package image 实现 core.image 图片组件（规范 docs/02-B §3/§4、02-C0、02-C5）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：媒体源（assetId 稳定引用）、
// 变体规格选择、构建期元数据解析（宽高/srcset/现代格式 source）与响应式 HTML 输出。
package image

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/media"
)

// Type 组件类型标识。
const Type = "core.image"

// Props 图片组件属性：媒体源 + 变体规格 + alt/sizes + 链接（Advanced 由基座处理）。
type Props struct {
	// AssetID 媒体资产稳定标识（组件树中唯一允许的图片引用方式）。
	AssetID string `json:"assetId" ct:"regex" ctRegex:"^[A-Za-z0-9_-]{4,64}$"`
	// Variant 绑定的变体规格：original / large / medium / thumbnail（默认 original）。
	Variant string `json:"variant,omitempty" ct:"select,original,large,medium,thumbnail,default=original"`
	// Alt 局部覆盖全局替代文本（默认继承全局 SEO 元数据）。
	Alt string `json:"alt,omitempty" ct:"text,maxlen=500"`
	// Title 局部覆盖全局标题。
	Title string `json:"title,omitempty" ct:"text,maxlen=500"`
	// Sizes srcset 的 sizes 提示（响应式布局宽度描述）。
	Sizes string `json:"sizes,omitempty" ct:"safe,maxlen=200"`
	// Link 可选包裹链接（点击跳转，协议白名单）。
	Link string `json:"link,omitempty" ct:"url"`
	// Advanced 通用高级属性（规范 docs/02-C0，基座约定字段）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 泛型基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName: Type,
		Render:   render,
	},
}

// render assetId → 媒体解析器 → 标准响应式 HTML + 链接包裹。
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
	if h.Media == nil {
		return "", fmt.Errorf("编译上下文缺少媒体解析器，无法解析 assetId")
	}
	if p.Variant == "" {
		p.Variant = media.VariantOriginal
	}
	meta, err := h.Media.ResolveMedia(p.AssetID, p.Variant)
	if err != nil {
		return "", err
	}
	if meta.Type != core.MediaTypeImage && meta.Type != core.MediaTypeSVG {
		return "", fmt.Errorf("媒体资产类型为 %s，图片组件仅支持图片类资产", meta.Type)
	}

	imgHTML, err := media.RenderImageHTML(meta, p.Alt, p.Title, media.ImageHTMLOptions{
		Class: h.Classes,
		Sizes: p.Sizes,
	})
	if err != nil {
		return "", err
	}

	if p.Link != "" {
		linkAttrs := `href="` + escapeHTML(p.Link) + `"`
		if h.CustomID != "" {
			linkAttrs += ` id="` + escapeHTML(h.CustomID) + `"`
		}
		return `<a ` + linkAttrs + `>` + imgHTML + `</a>`, nil
	}
	if h.CustomID != "" {
		// 无链接时自定义 ID 注入 <img>。
		imgHTML = strings.Replace(imgHTML, "<img ", "<img id=\""+escapeHTML(h.CustomID)+"\" ", 1)
	}
	return imgHTML, nil
}

// escapeHTML 属性值转义（image 包无 html 包引用，最小实现）。
func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// init 注册图片组件。
func init() {
	core.Register(Widget)
}