package video

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/url"
	"regexp"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.video"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("video", videoTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &Props{} }

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

var (
	youtubeRe = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/embed/)([\w-]{6,})`)
	vimeoRe   = regexp.MustCompile(`vimeo\.com/(\d+)`)
)

type Props struct {
	URL string `json:"url,omitempty" ct:"media,sec=content,label=视频地址"`

	Poster string `json:"poster,omitempty" ct:"media,sec=content,label=封面图"`

	Autoplay bool `json:"autoplay,omitempty" ct:"bool,sec=content,label=自动播放"`

	Loop bool `json:"loop,omitempty" ct:"bool,sec=content,label=循环播放"`

	Muted bool `json:"muted,omitempty" ct:"bool,sec=content,label=静音"`

	Controls bool `json:"controls,omitempty" ct:"bool,sec=content,label=播放控件"`

	CaptionsSrc string `json:"captionsSrc,omitempty" ct:"media,sec=content,label=字幕文件(VTT)"`

	CaptionsLabel string `json:"captionsLabel,omitempty" ct:"text,maxlen=40,sec=content,label=字幕名称"`

	CaptionsLang string `json:"captionsLang,omitempty" ct:"text,maxlen=20,sec=content,label=字幕语言"`

	Preload string `json:"preload,omitempty" ct:"select,metadata=元数据,auto=全部预加载,none=不预加载,default=metadata,sec=content,label=预加载"`

	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`

	FullWidth bool `json:"fullWidth,omitempty" ct:"bool,sec=style,label=全宽"`

	Ratio string `json:"ratio,omitempty" ct:"select,16:9=16:9,4:3=4:3,1:1=1:1,auto=自适应,default=16:9,sec=style,label=宽高比"`

	Radius string `json:"radius,omitempty" ct:"dimension,maxlen=20,sec=style,label=圆角"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

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

func embedURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if m := youtubeRe.FindStringSubmatch(raw); len(m) > 1 {
		q := ""
		return "https://www.youtube.com/embed/" + m[1] + q, true
	}
	if m := vimeoRe.FindStringSubmatch(raw); len(m) > 1 {
		return "https://player.vimeo.com/video/" + m[1], true
	}

	if u, err := url.Parse(raw); err == nil && strings.HasPrefix(raw, "https://") {
		if embedHostAllowed(u.Hostname()) {
			return raw, true
		}
	}
	return "", false
}

func embedHostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, allowed := range []string{"youtube.com", "youtu.be", "vimeo.com", "player.vimeo.com"} {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

//go:embed video.css
var videoCSS string

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

		panic(fmt.Sprintf("video 组件样式解析失败: %v", err))
	}
}

//go:embed video.jet
var videoTemplate string
