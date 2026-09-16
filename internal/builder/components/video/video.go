// Package video 实现 core.video：视频组件（对标 WD wd_video）。
// 支持：外部嵌入（YouTube / Vimeo / 通用 iframe）与本地 MP4（video 标签）。
package video

import (
	_ "embed" // video.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.video"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("video", videoTemplate)
}

// Component 视频组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Palette 实现 core.PaletteProvider：组件库呈现元数据（审计 REG-005）。
// 显示名 / 说明 / 分组 / 插入默认 Props 都在 Go 侧声明，前端只消费注入数据。
func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"url":      "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"controls": true,
			"ratio":    "16:9",
		},
		DisplayName: "视频",
		Hint:        "外链嵌入/本地 MP4",
	}
}

// embedRe 外链平台识别。
var (
	youtubeRe = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/embed/)([\w-]{6,})`)
	vimeoRe   = regexp.MustCompile(`vimeo\.com/(\d+)`)
)

// Props 视频属性。
type Props struct {
	// URL 视频地址：媒体库回填 URL / YouTube/Vimeo 链接（嵌入）或本地 MP4（/storage/…）。
	URL string `json:"url,omitempty" ct:"media,sec=content,label=视频地址"`
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
	// CaptionsSrc 字幕文件地址（WebVTT）。带语音的视频缺字幕，听障用户完全拿不到信息，
	// 搜索引擎也无法理解音轨内容。
	CaptionsSrc string `json:"captionsSrc,omitempty" ct:"media,sec=content,label=字幕文件(VTT)"`
	// CaptionsLabel 字幕轨道名称（播放器菜单显示，如「中文字幕」）。
	CaptionsLabel string `json:"captionsLabel,omitempty" ct:"text,maxlen=40,sec=content,label=字幕名称"`
	// CaptionsLang 字幕语言码（BCP 47，如 zh-CN）。
	CaptionsLang string `json:"captionsLang,omitempty" ct:"text,maxlen=20,sec=content,label=字幕语言"`
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
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
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
	if p.URL == "" {
		return fmt.Errorf("节点 %s: 请选择视频文件或填写视频地址", node.ID)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
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
	// 已是 /embed/ 形式的外链 iframe 直通（白名单域名精确匹配，防 evilyoutube.com 后缀绕过）。
	if u, err := url.Parse(raw); err == nil && strings.HasPrefix(raw, "https://") {
		if embedHostAllowed(u.Hostname()) {
			return raw, true
		}
	}
	return "", false
}

// embedHostAllowed 嵌入域名白名单：host 等于白名单域或为白名单域的子域。
// 必须精确匹配（suffix 前补 '.'），裸 strings.HasSuffix(host, "youtube.com")
// 会放行 evilyoutube.com / notyoutube.com；host 统一小写（URL host 大小写不敏感）。
func embedHostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, allowed := range []string{"youtube.com", "youtu.be", "vimeo.com", "player.vimeo.com"} {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// videoCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed video.css
var videoCSS string

// compileCSS 视频样式。
//
// 比例 → padding-top 百分比的映射留在 Go（是「预设名 → 数值」的翻译，不是样式组合）；
// 对齐的 margin 三条属于同一组，作为多声明值变量整组传入。
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

	alignDecls := "margin-left: auto; margin-right: auto"
	switch p.Align {
	case "left":
		alignDecls = "margin-left: 0; margin-right: auto"
	case "right":
		alignDecls = "margin-left: auto; margin-right: 0"
	}

	fullWidth := ""
	if p.FullWidth {
		fullWidth = "100%"
	}

	vars := map[string]string{
		"align_decls": alignDecls,
		"full_width":  fullWidth,
		"radius":      p.Radius,
		"pad":         pad,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, videoCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("video 组件样式解析失败: %v", err))
	}
}

// videoTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed video.jet
var videoTemplate string
