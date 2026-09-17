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
}

// themeFieldGroupView 模板渲染用的分组视图。
type themeFieldGroupView struct {
	Title  string
	Fields []themeFieldView
}

// themeFieldGroups 主题设置字段表（与后端 SaveThemeSettings 的点分键名对齐）。
var themeFieldGroups = []themeFieldGroup{
	{Title: "颜色", Fields: []themeField{
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
func buildThemeGroups(settings map[string]any) []themeFieldGroupView {
	groups := make([]themeFieldGroupView, 0, len(themeFieldGroups))
	for _, g := range themeFieldGroups {
		view := themeFieldGroupView{Title: g.Title, Fields: make([]themeFieldView, 0, len(g.Fields))}
		for _, f := range g.Fields {
			value := propString(settings, f.Path)
			item := themeFieldView{Label: f.Label, Path: f.Path, Name: f.Name, Kind: f.Kind, Value: value}
			for _, o := range f.Options {
				item.Options = append(item.Options, panelOption{Value: o.Value, Label: o.Label, Selected: o.Value == value})
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
	if themeID == "" {
		c.HTML(http.StatusOK, "fragments/global_panel", gin.H{"ThemeID": ""})
		return
	}
	var settings map[string]any
	if raw := c.PostForm("settings"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &settings)
	}
	c.HTML(http.StatusOK, "fragments/global_panel", gin.H{"ThemeID": themeID, "Groups": buildThemeGroups(settings)})
}
