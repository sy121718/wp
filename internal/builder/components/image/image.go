// Package image 实现 core.image 图片组件（规范 docs/02-B §3/§4、docs/02-C0）：
// 编辑器中仅记录 assetId（禁止硬编码临时物理路径），构建期通过媒体解析器
// 解析实际 URL、宽高、srcset 变体集合与全局 Alt，编译为标准响应式图片标签；
// 通用高级属性（间距/宽度对齐/边框圆角/阴影/透明度/显隐/class+ID）走 Advanced 层。
package image

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/media"
)

// Type 组件类型标识。
const Type = "core.image"

// assetIDRe assetId 白名单（media 包派生格式 ast_<hash 前 24 位>）。
var assetIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)

// urlRe 链接白名单：常见 URL 安全字符，禁止引号/尖括号/空白。
var urlRe = regexp.MustCompile(`^[A-Za-z0-9./:?=&%~#+_-]{1,500}$`)

// Props 图片组件能力描述：内容专属配置 + Advanced 通用层。
type Props struct {
	// AssetID 媒体资产稳定标识（组件树中唯一允许的图片引用方式）。
	AssetID string `json:"assetId"`
	// Variant 绑定的变体规格：original / large / medium / thumbnail（默认 original）。
	Variant string `json:"variant,omitempty"`
	// Alt 局部覆盖全局替代文本（默认继承全局 SEO 元数据）。
	Alt string `json:"alt,omitempty"`
	// Title 局部覆盖全局标题。
	Title string `json:"title,omitempty"`
	// Sizes srcset 的 sizes 提示（响应式布局宽度描述）。
	Sizes string `json:"sizes,omitempty"`
	// Link 可选包裹链接（点击跳转）。
	Link string `json:"link,omitempty"`
	// Advanced 通用高级属性（规范 docs/02-C0，全原子组件统一）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Image core.image 组件实现。
type Image struct{}

// Type 实现组件接口。
func (Image) Type() string { return Type }

// Validate 校验图片节点。叶子组件，不允许子节点。
func (Image) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err := validateNodeID(node.ID, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return errors.New("图片组件为叶子节点，不允许子节点")
	}

	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if !assetIDRe.MatchString(p.AssetID) {
		return fmt.Errorf("节点 %s: 无效的 assetId: %q", node.ID, p.AssetID)
	}
	if p.Variant == "" {
		p.Variant = media.VariantOriginal
	}
	if _, ok := map[string]bool{
		media.VariantOriginal:  true,
		media.VariantLarge:     true,
		media.VariantMedium:    true,
		media.VariantThumbnail: true,
	}[p.Variant]; !ok {
		return fmt.Errorf("节点 %s: 无效的变体规格: %q", node.ID, p.Variant)
	}
	if len(p.Alt) > 500 || len(p.Title) > 500 {
		return fmt.Errorf("节点 %s: 替代文本/标题过长（上限 500 字符）", node.ID)
	}
	if p.Sizes != "" && len(p.Sizes) > 200 {
		return fmt.Errorf("节点 %s: sizes 过长（上限 200 字符）", node.ID)
	}
	if p.Link != "" && !urlRe.MatchString(p.Link) {
		return fmt.Errorf("节点 %s: 无效的链接: %q", node.ID, p.Link)
	}

	// 通用高级属性校验（全原子组件共用一份规则）。
	return core.ValidateAdvanced(&p.Advanced, node.ID, ids)
}

// validateNodeID 节点 ID 白名单与唯一性。
func validateNodeID(id string, ids map[string]bool) (err error) {
	if len(id) < 1 || len(id) > 64 || !isIDChar(id) {
		return fmt.Errorf("无效的节点 ID: %q", id)
	}
	if ids[id] {
		return fmt.Errorf("节点 ID 重复: %q", id)
	}
	ids[id] = true
	return nil
}

// isIDChar 节点 ID 字符集校验（字母数字下划线连字符）。
func isIDChar(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// Render 渲染图片：assetId → 媒体解析器 → 标准响应式 HTML + Advanced CSS。
func (Image) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if ctx.Media == nil {
		return fmt.Errorf("节点 %s: 编译上下文缺少媒体解析器，无法解析 assetId", node.ID)
	}
	if p.Variant == "" {
		p.Variant = media.VariantOriginal
	}
	meta, err := ctx.Media.ResolveMedia(p.AssetID, p.Variant)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	if meta.Type != core.MediaTypeImage && meta.Type != core.MediaTypeSVG {
		return fmt.Errorf("节点 %s: 媒体资产类型为 %s，图片组件仅支持图片类资产", node.ID, meta.Type)
	}

	// Advanced 层：编译通用 CSS，取附加 class 与自定义 ID。
	extraClasses, customID := core.CompileAdvanced(node.ID, &p.Advanced, ctx.CSS)

	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	// 懒加载为默认开启（ImageHTMLOptions 零值即 lazy）。
	imgHTML, err := media.RenderImageHTML(meta, p.Alt, p.Title, media.ImageHTMLOptions{
		Class: strings.Join(classes, " "),
		Sizes: p.Sizes,
	})
	if err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	// 自定义 Element ID 附加在包裹链接上（若配置），否则注入 <img>。
	// RenderImageHTML 不支持 id 属性，这里以最小改动在后处理注入。
	if customID != "" && p.Link == "" {
		imgHTML = strings.Replace(imgHTML, "<img ", "<img id=\""+html.EscapeString(customID)+"\" ", 1)
	}

	if p.Link != "" {
		linkAttrs := `href="` + html.EscapeString(p.Link) + `"`
		if customID != "" {
			linkAttrs += ` id="` + html.EscapeString(customID) + `"`
		}
		imgHTML = `<a ` + linkAttrs + `>` + imgHTML + `</a>`
	}

	ctx.HTML.WriteString(imgHTML)
	return nil
}

// init 注册图片组件到编译内核。
func init() {
	core.Register(Image{})
}