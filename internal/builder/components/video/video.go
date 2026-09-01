// Package video 实现 core.video：视频组件（对标 WD wd_video）。
// 支持：外部嵌入（YouTube / Vimeo / 通用 iframe）与本地 MP4（video 标签）。
package video

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.video"

func init() { core.Register(&Component{}) }

// Component 视频组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// embedRe 外链平台识别。
var (
	youtubeRe = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/embed/)([\w-]{6,})`)
	vimeoRe   = regexp.MustCompile(`vimeo\.com/(\d+)`)
)

// Props 视频属性。
type Props struct {
	// AssetID 媒体库视频资产 ID（优先于 URL；构建期经 MediaResolver 解析为文件 URL）。
	AssetID string `json:"assetId,omitempty" ct:"media,sec=content,label=图片"`
	// URL 视频地址：YouTube/Vimeo 链接（嵌入）或本地 MP4（/storage/…）。
	URL string `json:"url,omitempty" ct:"url,sec=content,label=外链地址"`
	// Poster 封面图 URL（本地视频时显示）。
	Poster string `json:"poster,omitempty" ct:"media,sec=content,label=封面图"`
	// Autoplay 自动播放。
	Autoplay bool `json:"autoplay,omitempty" ct:"bool,sec=content,label=自动播放"`
	// Loop 循环播放。
	Loop bool `json:"loop,omitempty" ct:"bool,sec=content,label=循环播放"`
	// Muted 静音（自动播放通常需要）。
	Muted bool `json:"muted,omitempty" ct:"bool,sec=content,label=静音"`
	// Controls 显示播放控件。
	Controls bool `json:"controls,omitempty" ct:"bool,sec=content,label=播放控件"`
	// Preload 预加载策略：metadata（默认）/ auto / none。
	Preload string `json:"preload,omitempty" ct:"select,metadata=元数据,auto=全部预加载,none=不预加载,default=metadata,sec=content,label=预加载"`
	// Align 对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`
	// FullWidth 全宽。
	FullWidth bool `json:"fullWidth,omitempty" ct:"bool,sec=style,label=全宽"`
	// Ratio 宽高比：16:9 / 4:3 / 1:1 / 自适应。
	Ratio string `json:"ratio,omitempty" ct:"select,16:9=16:9,4:3=4:3,1:1=1:1,auto=自适应,default=16:9,sec=style,label=宽高比"`
	// Radius 圆角。
	Radius string `json:"radius,omitempty" ct:"dimension,maxlen=20,sec=style,label=圆角"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 视频为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.AssetID == "" && p.URL == "" {
		return fmt.Errorf("节点 %s: 请选择视频文件或填写外链地址", node.ID)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

// Render 渲染视频。
func (c *Component) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	cls := core.NodeClass(node.ID)

	ctx.HTML.WriteString(`<div class="`)
	ctx.HTML.WriteString(cls)
	ctx.HTML.WriteString(` wp-video">`)
	ctx.HTML.WriteString(`<div class="wp-video-frame">`)

	// 媒体库资产优先：构建期解析为文件 URL（video 类返回原文件）。
	videoURL := p.URL
	if p.AssetID != "" {
		if ctx.Media == nil {
			return fmt.Errorf("节点 %s: 编译上下文缺少媒体解析器，无法解析视频资产", node.ID)
		}
		meta, err := ctx.Media.ResolveMedia(p.AssetID, "")
		if err != nil {
			// 视频资产缺失：降级渲染占位，不阻塞整页编译。
			ctx.HTML.WriteString(`<div class="`)
			ctx.HTML.WriteString(core.NodeClass(node.ID))
			ctx.HTML.WriteString(` wp-video-missing"><span>视频缺失（媒体库已删除）</span></div></div>`)
			compileCSS(node.ID, &p, ctx.CSS)
			return nil
		}
		videoURL = meta.URL
	}
	// 本地文件（.mp4/.webm/.ogg 或 /storage/）→ video 标签；否则尝试外链嵌入。
	if embedSrc, ok := embedURL(videoURL); ok {
		// iframe 嵌入（YouTube/Vimeo）。
		ctx.HTML.WriteString(`<iframe src="`)
		ctx.HTML.WriteString(html.EscapeString(embedSrc))
		ctx.HTML.WriteString(`" allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture" allowfullscreen loading="lazy" title="视频"></iframe>`)
	} else {
		ctx.HTML.WriteString(`<video`)
		if p.Poster != "" {
			ctx.HTML.WriteString(` poster="`)
			ctx.HTML.WriteString(html.EscapeString(p.Poster))
			ctx.HTML.WriteString(`"`)
		}
		if p.Autoplay {
			ctx.HTML.WriteString(` autoplay`)
		}
		if p.Loop {
			ctx.HTML.WriteString(` loop`)
		}
		if p.Muted || p.Autoplay {
			ctx.HTML.WriteString(` muted`)
		}
		if p.Controls {
			ctx.HTML.WriteString(` controls`)
		}
		preload := p.Preload
		if preload == "" {
			preload = "metadata"
		}
		ctx.HTML.WriteString(` playsinline preload="` + preload + `">`)
		ctx.HTML.WriteString(`<source src="`)
		ctx.HTML.WriteString(html.EscapeString(videoURL))
		ctx.HTML.WriteString(`">`)
		ctx.HTML.WriteString(`</video>`)
	}

	ctx.HTML.WriteString(`</div></div>`)

	compileCSS(node.ID, &p, ctx.CSS)
	return nil
}

// embedURL 识别外链平台并返回可嵌入 URL；无法识别返回 ok=false。
func embedURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if m := youtubeRe.FindStringSubmatch(raw); len(m) > 1 {
		q := ""
		return "https://www.youtube.com/embed/" + m[1] + q, true
	}
	if m := vimeoRe.FindStringSubmatch(raw); len(m) > 1 {
		return "https://player.vimeo.com/video/" + m[1], true
	}
	// 已是 /embed/ 形式的外链 iframe 直通（白名单域名）。
	if u, err := url.Parse(raw); err == nil && strings.HasPrefix(raw, "https://") {
		host := u.Hostname()
		if strings.HasSuffix(host, "youtube.com") || strings.HasSuffix(host, "youtu.be") ||
			strings.HasSuffix(host, "vimeo.com") || strings.HasSuffix(host, "player.vimeo.com") {
			return raw, true
		}
	}
	return "", false
}

// compileCSS 视频样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	ratio := p.Ratio
	if ratio == "" {
		ratio = "16:9"
	}
	var pad string
	switch ratio {
	case "4:3":
		pad = "75%"
	case "1:1":
		pad = "100%"
	case "auto":
		pad = ""
	default:
		pad = "56.25%"
	}

	desktop := []string{"position: relative", "overflow: hidden", "max-width: 100%"}
	switch p.Align {
	case "left":
		desktop = append(desktop, "margin-left: 0", "margin-right: auto")
	case "right":
		desktop = append(desktop, "margin-left: auto", "margin-right: 0")
	default:
		desktop = append(desktop, "margin-left: auto", "margin-right: auto")
	}
	if p.FullWidth {
		desktop = append(desktop, "width: 100%")
	}
	if p.Radius != "" {
		desktop = append(desktop, "border-radius: "+p.Radius)
	}
	b.Add(core.BreakpointDesktop, sel, desktop)

	frame := []string{"position: relative", "width: 100%"}
	if pad != "" {
		frame = append(frame, "padding-top: "+pad)
	}
	b.Add(core.BreakpointDesktop, sel+" .wp-video-frame", frame)
	b.Add(core.BreakpointDesktop, sel+" .wp-video-frame iframe, "+sel+" .wp-video-frame video", []string{
		"position: absolute", "inset: 0", "width: 100%", "height: 100%",
		"border: 0", "border-radius: inherit", "display: block",
	})
}
