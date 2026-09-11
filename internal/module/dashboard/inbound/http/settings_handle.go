package dashboardhttp

// settings_handle.go — 页面设置面板的服务端渲染（HTMX 化，docs/09 §3）。
//
// 背景：workbench.js 的 renderSettingsPanel 用 150+ 行 DOM 代码拼「版心/SEO 字段 +
// segment 组 + 评分面板 + SERP 预览」。本文件把表单与评分都搬到服务端（Jet 片段），
// 客户端只保留「值变更 → 回写 doc.settings → 刷新画布/评分」。
//
// 端点：
//   POST /workbench/settings         → 表单片段（含评分容器占位）
//   POST /workbench/seo-score-panel  → 评分区片段（字段改动后局部刷新）

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	seoscore "go_wp/internal/seo"

	"github.com/gin-gonic/gin"
)

// settingsView 页面设置表单数据（settings.layout / settings.seo 的子集）。
type settingsView struct {
	LayoutMode        string
	MainLandmark      bool
	SEOTitle          string
	SEODescription    string
	FocusKeyword      string
	Canonical         string
	OGImage           string
	SchemaType        string
	RobotsIndex       string
	RobotsFollow      string
	SecondaryKeywords string
	Intent            string
	// ThemeOverrides 页面级主题覆盖字段（留空 = 跟随站点主题）。
	ThemeOverrides []themeOverrideFieldView
}

// themeOverrideFieldView 页面级主题覆盖的一个字段。
type themeOverrideFieldView struct {
	Label string
	// Path 回写路径：settings.themeOverride.<主题令牌路径>。
	Path string
	// Value 页面当前覆盖值（空 = 未覆盖，跟随站点主题）。
	Value string
}

// themeOverrideKinds 允许页面级覆盖的字段类型。
//
// 先只放颜色：排版/间距这类令牌页面级覆盖的实际需求低，字段一多面板就没法用。
// 合并逻辑本身是对全部令牌通用的（MergeThemeRawJSON 按 JSON 键合并），
// 想放开哪一类，往这里加一个 kind 即可。
var themeOverrideKinds = map[string]bool{"color": true}

// themeOverrideFields 从主题字段表里挑出可覆盖项，并回填页面当前值。
func themeOverrideFields(overrides map[string]any) []themeOverrideFieldView {
	var out []themeOverrideFieldView
	for _, g := range themeFieldGroups {
		for _, f := range g.Fields {
			if !themeOverrideKinds[f.Kind] {
				continue
			}
			out = append(out, themeOverrideFieldView{
				Label: g.Title + " · " + f.Label,
				Path:  "settings.themeOverride." + f.Path,
				Value: themePathString(overrides, f.Path),
			})
		}
	}
	return out
}

// themePathString 按点分路径从覆盖对象里取值（不存在或非字符串返回空）。
func themePathString(obj map[string]any, path string) string {
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

// scoreView SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type scoreView struct {
	OK        bool
	Total     int
	Grade     string
	Sections  []scoreSectionView
	SerpTitle string
	SerpURL   string
	SerpDesc  string
}

// scoreSectionView 单个评分维度。
type scoreSectionView struct {
	Label      string
	Color      string
	ColorLabel string
	Score      int
	Max        int
	Issues     []scoreIssueView
}

// scoreIssueView 单条未达标检查（Target 非空表示可点击定位）。
type scoreIssueView struct {
	Text   string
	Target string
}

// seoColorLabels 评分颜色 → 中文（与前端旧面板一致）。
var seoColorLabels = map[string]string{
	"green": "优秀", "lightgreen": "良好", "yellow": "需改进", "red": "差", "red-blocking": "缺失",
}

// SettingsPanel 渲染页面设置表单片段。
func (h *Handle) SettingsPanel(c *gin.Context) {
	doc := json.RawMessage(c.PostForm("document"))
	c.HTML(http.StatusOK, "fragments/settings_panel", gin.H{"Settings": settingsViewOf(doc)})
}

// SeoScorePanel 渲染 SEO 评分区片段（字段改动后局部刷新，避免整块表单重绘丢焦点）。
func (h *Handle) SeoScorePanel(c *gin.Context) {
	doc := json.RawMessage(c.PostForm("document"))
	c.HTML(http.StatusOK, "fragments/seo_score", gin.H{"Score": scoreViewOf(doc, c.PostForm("url"))})
}

// settingsViewOf 解析草稿文档的 settings 片段。
func settingsViewOf(doc json.RawMessage) settingsView {
	var parsed struct {
		Settings struct {
			Layout struct {
				Mode         string `json:"mode"`
				MainLandmark bool   `json:"mainLandmark"`
			} `json:"layout"`
			SEO struct {
				Title             string   `json:"title"`
				Description       string   `json:"description"`
				FocusKeyword      string   `json:"focusKeyword"`
				Canonical         string   `json:"canonical"`
				OGImage           string   `json:"ogImage"`
				SchemaType        string   `json:"schemaType"`
				RobotsIndex       string   `json:"robotsIndex"`
				RobotsFollow      string   `json:"robotsFollow"`
				SecondaryKeywords []string `json:"secondaryKeywords"`
				Intent            string   `json:"intent"`
			} `json:"seo"`
			ThemeOverride map[string]any `json:"themeOverride"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(doc, &parsed)
	mode := parsed.Settings.Layout.Mode
	if mode == "" {
		mode = "full"
	}
	intent := parsed.Settings.SEO.Intent
	if intent == "" {
		intent = "informational"
	}
	return settingsView{
		LayoutMode:        mode,
		MainLandmark:      parsed.Settings.Layout.MainLandmark,
		SEOTitle:          parsed.Settings.SEO.Title,
		SEODescription:    parsed.Settings.SEO.Description,
		FocusKeyword:      parsed.Settings.SEO.FocusKeyword,
		Canonical:         parsed.Settings.SEO.Canonical,
		OGImage:           parsed.Settings.SEO.OGImage,
		SchemaType:        parsed.Settings.SEO.SchemaType,
		RobotsIndex:       parsed.Settings.SEO.RobotsIndex,
		RobotsFollow:      parsed.Settings.SEO.RobotsFollow,
		SecondaryKeywords: strings.Join(parsed.Settings.SEO.SecondaryKeywords, " "),
		Intent:            intent,
		ThemeOverrides:    themeOverrideFields(parsed.Settings.ThemeOverride),
	}
}

// scoreViewOf 计算并转换 SEO 评分（文档为空/评分失败时返回 OK=false，模板显示空态）。
func scoreViewOf(doc json.RawMessage, pageURL string) scoreView {
	if len(doc) == 0 {
		return scoreView{}
	}
	res, err := seoscore.ScoreDocument(doc, pageURL)
	if err != nil || res == nil {
		return scoreView{}
	}
	sv := scoreView{OK: true, Total: res.Total, Grade: res.Grade}
	for _, sec := range res.Sections {
		item := scoreSectionView{
			Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
			ColorLabel: seoColorLabels[sec.Color],
		}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, scoreIssueView{
				Text:   fmt.Sprintf("%s：%s（基准 %s）→ %s", ck.Label, ck.Actual, ck.Benchmark, ck.Hint),
				Target: ck.Target,
			})
		}
		sv.Sections = append(sv.Sections, item)
	}
	// SERP 预览取页面设置里的标题/描述（空则占位）。
	sv.SerpTitle = settingsViewOf(doc).SEOTitle
	if sv.SerpTitle == "" {
		sv.SerpTitle = "（未填写 SEO 标题）"
	}
	sv.SerpDesc = settingsViewOf(doc).SEODescription
	if sv.SerpDesc == "" {
		sv.SerpDesc = "（未填写 SEO 描述）"
	}
	sv.SerpURL = pageURL
	if sv.SerpURL == "" {
		sv.SerpURL = "https://example.com/page"
	}
	return sv
}
