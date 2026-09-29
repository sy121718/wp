package projecthttp

// theme_field_groups.go — 主题设置字段表与全局设置面板渲染
// （自 dashboard/inbound/http/global_handle.go 迁入）。
//
// 背景：workbench.js 的 renderGlobalPanel 用 100+ 行 DOM 代码拼 43 个主题字段
//（色板/排版/按钮/表面/动效）。本文件把字段定义与渲染搬到服务端；客户端只保留
// 「取色器增强 + 保存到 /admin/themes/settings/save」。
//
// 端点：POST /workbench/global（form: settings = themeSettings JSON、themeId）。

import (
	"encoding/json"
	"net/http"
	"strings"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"
)

// panelOption 选项下拉项（与迁移前 inspectorOption 同形：模板按字段名访问）。
type panelOption struct {
	Value    string
	Label    string
	Selected bool
}

// themeField 主题设置字段：[显示名, 回显路径(JSON 字段名), 提交键名, 控件, 选项]。
// 回显路径读 themeSettings；提交键名是后端 SaveThemeSettings 的 PostForm 点分约定。
type themeField struct {
	Label   string
	Path    string
	Name    string
	Kind    string // text / color / select
	Options []panelOption
}

// themeFieldGroup 主题设置字段分组。
type themeFieldGroup struct {
	Title  string
	Fields []themeField
}

// themeFieldView 模板渲染用的字段视图（含当前值）。
type themeFieldView struct {
	Label string
	// Path 回显路径（themeSettings JSON 字段名，取色器槽用）。
	Path    string
	Name    string
	Kind    string
	Value   string
	Options []panelOption
	// Invalid / Error 保存失败后的**字段级**错误态：Invalid 让控件输出 aria-invalid="true"
	// （基座 ui.css 用它画红框，读屏也会播报），Error 是控件下方的一行红字（.form-error）。
	//
	// 只有主题设置页会用它们：工作台的全局设置面板复用同一个字段视图，
	// 那里的字段始终是零值（无错可报），模板不会渲染出多余节点。
	Invalid bool
	Error   string
}

// themeFieldGroupView 模板渲染用的分组视图。
type themeFieldGroupView struct {
	Title  string
	Fields []themeFieldView
}

// themeLabelKeys 主题设置的**展示文案**（分组标题 / 字段 label / 下拉选项 label）→ i18n key。
//
// 为什么不给 themeField / panelOption 各加一个 Key 字段：同一段文本在三处只是「同一句话」
// （「颜色」既是分组标题也是字段 label，「默认」在十几个下拉里反复出现），按文本查表
// 让它们自然共享一条词条；逐条加字段则会造出 90 个只在某一处成立的 key，
// 改一次措辞要动 90 行表。
//
// **表里没有的文本 = 不翻译**（如 value 与 label 相同的 "20px"、"Inter, system-ui, sans-serif"），
// 取词函数收到空 key 时原样返回 label —— 这正是我们要的行为，不需要为它们造 key。
var themeLabelKeys = map[string]string{
	// —— 分组标题（同时是字段 label 的按同一行取词）——
	"颜色":      "admin.theme_settings.label.colors",
	"排版 · 标题": "admin.theme_settings.label.typographyHeading",
	"排版 · 正文": "admin.theme_settings.label.typographyBody",
	"排版 · 链接": "admin.theme_settings.label.typographyLink",
	"按钮":      "admin.theme_settings.label.button",
	"表面":      "admin.theme_settings.label.surface",
	"图片":      "admin.theme_settings.label.images",
	"动效":      "admin.theme_settings.label.motion",
	// —— 字段 label ——
	"主色":     "admin.theme_settings.label.primaryColor",
	"次色":     "admin.theme_settings.label.secondaryColor",
	"点缀色":    "admin.theme_settings.label.accentColor",
	"成功色":    "admin.theme_settings.label.successColor",
	"警告色":    "admin.theme_settings.label.warningColor",
	"危险色":    "admin.theme_settings.label.dangerColor",
	"正文色":    "admin.theme_settings.label.bodyColor",
	"标题色":    "admin.theme_settings.label.headingColor",
	"页面背景":   "admin.theme_settings.label.pageBackground",
	"卡片底色":   "admin.theme_settings.label.cardSurface",
	"边框色":    "admin.theme_settings.label.borderColor",
	"字重":     "admin.theme_settings.label.fontWeight",
	"基准字号":   "admin.theme_settings.label.headingSize",
	"标题下间距":  "admin.theme_settings.label.headingSpacing",
	"字体":     "admin.theme_settings.label.fontFamily",
	"字号":     "admin.theme_settings.label.bodySize",
	"行高":     "admin.theme_settings.label.lineHeight",
	"悬停色":    "admin.theme_settings.label.linkHoverColor",
	"下划线":    "admin.theme_settings.label.underline",
	"背景":     "admin.theme_settings.label.buttonBackground",
	"文字色":    "admin.theme_settings.label.buttonTextColor",
	"圆角":     "admin.theme_settings.label.radius",
	"纵向内边距":  "admin.theme_settings.label.paddingY",
	"横向内边距":  "admin.theme_settings.label.paddingX",
	"悬停背景":   "admin.theme_settings.label.hoverBackground",
	"悬停文字色":  "admin.theme_settings.label.hoverTextColor",
	"边框宽":    "admin.theme_settings.label.borderWidth",
	"边框样式":   "admin.theme_settings.label.borderStyle",
	"阴影":     "admin.theme_settings.label.shadow",
	"全局圆角":   "admin.theme_settings.label.globalRadius",
	"默认阴影":   "admin.theme_settings.label.defaultShadow",
	"懒加载默认":  "admin.theme_settings.label.lazyDefault",
	"懒加载骨架屏": "admin.theme_settings.label.lazySkeleton",
	"过渡时长":   "admin.theme_settings.label.transitionDuration",
	"缓动":     "admin.theme_settings.label.easing",
	"默认入场":   "admin.theme_settings.label.entrance",
	// —— 下拉选项 label ——
	"默认":            "admin.theme_settings.label.optDefault",
	"无":             "admin.theme_settings.label.optNone",
	"小":             "admin.theme_settings.label.optSmall",
	"中":             "admin.theme_settings.label.optMedium",
	"大":             "admin.theme_settings.label.optLarge",
	"特大":            "admin.theme_settings.label.optXLarge",
	"直角":            "admin.theme_settings.label.optRadiusNone",
	"胶囊":            "admin.theme_settings.label.optRadiusPill",
	"实线":            "admin.theme_settings.label.optBorderSolid",
	"虚线":            "admin.theme_settings.label.optBorderDashed",
	"点线":            "admin.theme_settings.label.optBorderDotted",
	"双线":            "admin.theme_settings.label.optBorderDouble",
	"无边框":           "admin.theme_settings.label.optBorderNone",
	"悬停时":           "admin.theme_settings.label.optUnderlineHover",
	"始终":            "admin.theme_settings.label.optUnderlineAlways",
	"开启":            "admin.theme_settings.label.optOn",
	"关闭":            "admin.theme_settings.label.optOff",
	"开启（推荐）":        "admin.theme_settings.label.optOnRecommended",
	"常规":            "admin.theme_settings.label.optWeightRegular",
	"中等":            "admin.theme_settings.label.optWeightMedium",
	"半粗":            "admin.theme_settings.label.optWeightSemiBold",
	"粗体":            "admin.theme_settings.label.optWeightBold",
	"淡入":            "admin.theme_settings.label.optFadeIn",
	"淡入·上":          "admin.theme_settings.label.optFadeUp",
	"上滑":            "admin.theme_settings.label.optSlideUp",
	"缩放":            "admin.theme_settings.label.optZoomIn",
	"无（瞬时）":         "admin.theme_settings.label.optDurationNone",
	"8px（推荐）":       "admin.theme_settings.label.optRadius8Recommended",
	"10px（推荐）":      "admin.theme_settings.label.optRadius10Recommended",
	"12px（推荐）":      "admin.theme_settings.label.optRadius12Recommended",
	"16px（推荐）":      "admin.theme_settings.label.optSize16Recommended",
	"20px（推荐）":      "admin.theme_settings.label.optSize20Recommended",
	"32px（推荐）":      "admin.theme_settings.label.optSize32Recommended",
	"200ms（推荐）":     "admin.theme_settings.label.optDuration200Recommended",
	"1.4（紧凑）":       "admin.theme_settings.label.optLineHeight14Tight",
	"1.6（常用）":       "admin.theme_settings.label.optLineHeight16Common",
	"1.7（推荐）":       "admin.theme_settings.label.optLineHeight17Recommended",
	"1.8（宽松）":       "admin.theme_settings.label.optLineHeight18Loose",
	"系统默认（推荐）":      "admin.theme_settings.label.optFontSystemRecommended",
	"默认（跟随主题）":      "admin.theme_settings.label.optFontThemeDefault",
	"Inter / 现代无衬线": "admin.theme_settings.label.optFontInter",
	"Georgia（衬线）":   "admin.theme_settings.label.optFontGeorgia",
	"衬线体":           "admin.theme_settings.label.optFontSerif",
	"等宽体":           "admin.theme_settings.label.optFontMono",
	"中文无衬线":         "admin.theme_settings.label.optFontChineseSans",
}

// themeLabelText 展示文案 → 当前语言文案；表里没有的文本原样返回（见 themeLabelKeys 注释）。
func themeLabelText(tr func(key, fallback string) string, label string) string {
	return tr(themeLabelKeys[label], label)
}

// themeFieldGroups 主题设置字段表（与后端 SaveThemeSettings 的点分键名对齐）。
var themeFieldGroups = []themeFieldGroup{{Title: "颜色", Fields: []themeField{
	{Label: "主色", Path: "colors.primary", Name: "colors.primary", Kind: "color"},
	{Label: "次色", Path: "colors.secondary", Name: "colors.secondary", Kind: "color"},
	{Label: "点缀色", Path: "colors.accent", Name: "colors.accent", Kind: "color"},
	{Label: "成功色", Path: "colors.success", Name: "colors.success", Kind: "color"},
	{Label: "警告色", Path: "colors.warning", Name: "colors.warning", Kind: "color"},
	{Label: "危险色", Path: "colors.danger", Name: "colors.danger", Kind: "color"},
	{Label: "正文色", Path: "colors.text", Name: "colors.text", Kind: "color"},
	{Label: "标题色", Path: "colors.heading", Name: "colors.heading", Kind: "color"},
	{Label: "页面背景", Path: "colors.background", Name: "colors.background", Kind: "color"},
	{Label: "卡片底色", Path: "colors.surface", Name: "colors.surface", Kind: "color"},
	{Label: "边框色", Path: "colors.border", Name: "colors.border", Kind: "color"},
}},
	{Title: "排版 · 标题", Fields: []themeField{
		{Label: "颜色", Path: "typography.heading.color", Name: "typography.heading.color", Kind: "color"},
		{Label: "字重", Path: "typography.heading.fontWeight", Name: "typography.heading.weight", Kind: "select",
			Options: weightOptions()},
		{Label: "基准字号", Path: "typography.heading.fontSize", Name: "typography.heading.size", Kind: "select",
			Options: optionsOf("", "默认", "20px", "20px", "24px", "24px", "28px", "28px", "32px", "32px（推荐）", "36px", "36px", "40px", "40px", "48px", "48px", "56px", "56px")},
		{Label: "标题下间距", Path: "typography.heading.spacing", Name: "typography.heading.spacing", Kind: "select",
			Options: optionsOf("", "默认", "0px", "0", "4px", "4px", "8px", "8px", "12px", "12px（推荐）", "16px", "16px", "24px", "24px", "32px", "32px")},
		{Label: "字体", Path: "typography.heading.fontFamily", Name: "typography.heading.font", Kind: "datalist",
			Options: fontOptions()},
	}},
	{Title: "排版 · 正文", Fields: []themeField{
		{Label: "颜色", Path: "typography.body.color", Name: "typography.body.color", Kind: "color"},
		{Label: "字号", Path: "typography.body.fontSize", Name: "typography.body.size", Kind: "select",
			Options: optionsOf("", "默认", "14px", "14px", "15px", "15px", "16px", "16px（推荐）", "17px", "17px", "18px", "18px", "20px", "20px")},
		{Label: "行高", Path: "typography.body.lineHeight", Name: "typography.body.line", Kind: "select",
			Options: optionsOf("", "默认", "1.4", "1.4（紧凑）", "1.5", "1.5", "1.6", "1.6（常用）", "1.7", "1.7（推荐）", "1.8", "1.8（宽松）", "2", "2.0")},
		{Label: "字体", Path: "typography.body.fontFamily", Name: "typography.body.font", Kind: "datalist",
			Options: fontOptions()},
	}},
	{Title: "排版 · 链接", Fields: []themeField{
		{Label: "颜色", Path: "typography.link.color", Name: "typography.link.color", Kind: "color"},
		{Label: "悬停色", Path: "typography.link.hoverColor", Name: "typography.link.hover", Kind: "color"},
		{Label: "下划线", Path: "typography.link.underline", Name: "typography.link.underline", Kind: "select",
			Options: optionsOf("", "默认", "none", "无", "hover", "悬停时", "always", "始终")},
	}},
	{Title: "按钮", Fields: []themeField{
		{Label: "背景", Path: "button.background", Name: "button.background", Kind: "color"},
		{Label: "文字色", Path: "button.color", Name: "button.color", Kind: "color"},
		{Label: "圆角", Path: "button.radius", Name: "button.radius", Kind: "select",
			Options: optionsOf("", "默认", "0", "直角", "4px", "4px", "6px", "6px", "8px", "8px（推荐）", "12px", "12px", "16px", "16px", "999px", "胶囊")},
		{Label: "字重", Path: "button.fontWeight", Name: "button.weight", Kind: "select", Options: weightOptions()},
		{Label: "纵向内边距", Path: "button.paddingY", Name: "button.py", Kind: "select",
			Options: optionsOf("", "默认", "6px", "6px", "8px", "8px", "10px", "10px（推荐）", "12px", "12px", "14px", "14px", "16px", "16px")},
		{Label: "横向内边距", Path: "button.paddingX", Name: "button.px", Kind: "select",
			Options: optionsOf("", "默认", "12px", "12px", "16px", "16px", "20px", "20px（推荐）", "24px", "24px", "28px", "28px", "32px", "32px")},
		{Label: "悬停背景", Path: "button.hoverBackground", Name: "button.hoverBg", Kind: "color"},
		{Label: "悬停文字色", Path: "button.hoverColor", Name: "button.hoverColor", Kind: "color"},
		{Label: "边框宽", Path: "button.borderWidth", Name: "button.borderWidth", Kind: "select",
			Options: optionsOf("", "默认", "0", "无边框", "1px", "1px", "2px", "2px", "3px", "3px", "4px", "4px")},
		{Label: "边框色", Path: "button.borderColor", Name: "button.borderColor", Kind: "color"},
		{Label: "边框样式", Path: "button.borderStyle", Name: "button.borderStyle", Kind: "select",
			Options: optionsOf("", "默认", "solid", "实线", "dashed", "虚线", "dotted", "点线", "double", "双线")},
		{Label: "阴影", Path: "button.shadow", Name: "button.shadow", Kind: "select",
			Options: optionsOf("", "无", "sm", "小", "md", "中", "lg", "大", "xl", "特大")},
	}},
	{Title: "表面", Fields: []themeField{
		{Label: "全局圆角", Path: "surface.radius", Name: "surface.radius", Kind: "select",
			Options: optionsOf("", "默认", "0", "直角", "6px", "6px", "8px", "8px", "10px", "10px（推荐）", "12px", "12px", "16px", "16px")},
		{Label: "边框宽", Path: "surface.borderWidth", Name: "surface.borderWidth", Kind: "select",
			Options: optionsOf("", "默认", "0", "无边框", "1px", "1px", "2px", "2px", "3px", "3px")},
		{Label: "边框色", Path: "surface.borderColor", Name: "surface.borderColor", Kind: "color"},
		{Label: "默认阴影", Path: "surface.shadow", Name: "surface.shadow", Kind: "select",
			Options: optionsOf("", "无", "sm", "小", "md", "中", "lg", "大", "xl", "特大")},
	}},
	{Title: "图片", Fields: []themeField{
		// 懒加载默认策略：组件级「图片加载」为「默认」时继承这里。
		{Label: "懒加载默认", Path: "images.lazyLoad", Name: "images.lazyLoad", Kind: "select",
			Options: optionsOf("on", "开启（推荐）", "off", "关闭")},
		// 骨架屏：懒加载图片显示纯 CSS 渐变占位（加载完成后自然覆盖）。
		{Label: "懒加载骨架屏", Path: "images.skeleton", Name: "images.skeleton", Kind: "select",
			Options: optionsOf("false", "关闭", "true", "开启")},
	}},
	{Title: "动效", Fields: []themeField{
		{Label: "过渡时长", Path: "motion.transitionDuration", Name: "motion.duration", Kind: "select",
			Options: optionsOf("", "默认", "0ms", "无（瞬时）", "100ms", "100ms", "150ms", "150ms", "200ms", "200ms（推荐）", "300ms", "300ms", "500ms", "500ms")},
		{Label: "缓动", Path: "motion.easing", Name: "motion.easing", Kind: "select",
			Options: optionsOf("", "默认", "ease", "Ease", "ease-out", "Ease Out", "linear", "Linear")},
		{Label: "默认入场", Path: "motion.defaultEntrance", Name: "motion.entrance", Kind: "select",
			Options: optionsOf("", "无", "fade-in", "淡入", "fade-up", "淡入·上", "slide-up", "上滑", "zoom-in", "缩放")},
	}},
}

// fontOptions 字体栈预设：可选项直接选，也可以自己填（模板用 datalist，输入框仍可自由输入）。
func fontOptions() []panelOption {
	return optionsOf(
		"", "默认（跟随主题）",
		"system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif", "系统默认（推荐）",
		"Inter, system-ui, sans-serif", "Inter / 现代无衬线",
		"Georgia, serif", "Georgia（衬线）",
		"ui-serif, Georgia, Times New Roman, serif", "衬线体",
		"ui-monospace, SFMono-Regular, Menlo, monospace", "等宽体",
		"PingFang SC, Microsoft YaHei, Noto Sans SC, sans-serif", "中文无衬线",
	)
}

// weightOptions 字重选项（默认 + 400~700）。
func weightOptions() []panelOption {
	return optionsOf("", "默认", "400", "常规", "500", "中等", "600", "半粗", "700", "粗体")
}

// optionsOf 把 value,label 交替参数转成选项列表。
func optionsOf(pairs ...string) []panelOption {
	out := make([]panelOption, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, panelOption{Value: pairs[i], Label: pairs[i+1]})
	}
	return out
}

// buildThemeGroups 按字段表把主题设置摊成可渲染的分组视图（含当前值与选中项）。
//
// 工作台的全局设置面板与后台的主题设置页共用这一份 —— 控件类型（下拉/取色器/数值档位）
// 只在这里定义一次，避免两边各写一套、改了这头忘那头。
//
// tr 为可选取词函数（不传时按中文原文渲染，见 themeLabelKeys）：分组标题、字段 label
// 与选项 label 全部经它取词，字段表本身只留中文原文。
func buildThemeGroups(settings map[string]any, trs ...func(key, fallback string) string) []themeFieldGroupView {
	tr := func(_, fallback string) string { return fallback }
	if len(trs) > 0 && trs[0] != nil {
		tr = trs[0]
	}
	groups := make([]themeFieldGroupView, 0, len(themeFieldGroups))
	for _, g := range themeFieldGroups {
		view := themeFieldGroupView{Title: themeLabelText(tr, g.Title), Fields: make([]themeFieldView, 0, len(g.Fields))}
		for _, f := range g.Fields {
			value := propString(settings, f.Path)
			item := themeFieldView{Label: themeLabelText(tr, f.Label), Path: f.Path, Name: f.Name, Kind: f.Kind, Value: value}
			for _, o := range f.Options {
				item.Options = append(item.Options, panelOption{Value: o.Value, Label: themeLabelText(tr, o.Label), Selected: o.Value == value})
			}
			view.Fields = append(view.Fields, item)
		}
		groups = append(groups, view)
	}
	return groups
}

// propString 按点分路径从对象里取字符串值（不存在或非字符串返回空）。
func propString(obj map[string]any, path string) string {
	var cur any = obj
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[seg]
		if !ok {
			return ""
		}
	}
	s, _ := cur.(string)
	return s
}

// workbenchGlobalPanel 渲染全局设置（站点主题）面板片段（POST /workbench/global）。
func workbenchGlobalPanel(c *gin.Context) {
	themeID := strings.TrimSpace(c.PostForm("themeId"))
	// t 是片段模板的取词函数：片段不经 shell.Prepare，缺 t 时 Jet 把取词调用求值成空串。
	tr := shell.TranslateFor(c)
	if themeID == "" {
		c.HTML(http.StatusOK, "fragments/global_panel", gin.H{"ThemeID": "", "t": tr})
		return
	}
	var settings map[string]any
	if raw := c.PostForm("settings"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &settings)
	}
	c.HTML(http.StatusOK, "fragments/global_panel",
		gin.H{"ThemeID": themeID, "Groups": buildThemeGroups(settings, tr), "t": tr})
}
