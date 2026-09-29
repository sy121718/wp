package projecthttp

// settings_panel.go — 工作台页面设置面板与 SEO 评分区（自 dashboard/inbound/http/settings_handle.go 迁入）。
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
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go_wp/internal/builder"
	projectenums "go_wp/internal/module/project/enums"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

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
	// AccessType 访问权限类型（PIPE-6）："" / public / password / members。
	AccessType string
	// AccessPasswordSet 是否已设置访问密码。
	//
	// **刻意只回传布尔**：bcrypt 哈希绝不回填进模板的 value 属性。面板片段会经
	// morphHTML 整块替换、也可能被浏览器缓存或开发者工具留存，把哈希烘进 HTML
	// 等于把它放进页面源码、浏览器历史与任何一份「保存的网页」里。
	AccessPasswordSet bool
	// AccessHash 仅当**本次请求刚设置了密码**时非空：它是服务端算出的新哈希，
	// 借隐藏字段回填进客户端文档（data-wb-apply 只应用一次）。
	// 未设置密码时为空 —— 哈希不进 value 的原则在这里同样成立。
	AccessHash string
	// AccessError 密码设置失败的可读原因（超长等）；空 = 无错误。
	AccessError string
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
	OK       bool
	Total    int
	Grade    string
	Sections []scoreSectionView
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重（页面草稿与文章）。
	ProfileType   string
	ProfileReason string
	// Duplicates 与本页标题重复的其它页面（审计 SEO-018 编辑期轻量版）。
	// **列出冲突页面本身**而不是只报数量：只报「有重复」运营不知道该去改哪一页。
	Duplicates    []string
	DuplicateNote string
	// Empty 空态文案（读不到实体时给一句可读的话，而不是让面板整体不可用）。
	Empty     string
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

// 评分颜色 → 等级文案的映射**只有一份**，在 seoscore.ScoreGradeText（internal/seo/score_grade.go）。
// 此前本文件与 content/inbound/http/article_score_view.go 各持一份逐字相同的副本，
// 两处注释都写着“正确的归宿是 seoscore 包” —— 已按此收编，取词改调该出口。

// workbenchSettingsPanel 渲染页面设置表单片段（POST /workbench/settings）。
//
// 该端点同时承担「访问密码重设」（PIPE-6）：表单里的 access-password 非空时，
// 服务端把它 bcrypt 成本次响应的 settings.access.passwordHash，并借一个
// data-wb-apply 的隐藏字段回填到客户端文档。**不新增路由**——重设密码与渲染
// 面板是同一个往返（同一次 document 解析、同一次片段替换），拆成两个端点只会
// 多出一份「谁的 document 更新」的一致性维护。
func workbenchSettingsPanel(c *gin.Context) {
	doc := json.RawMessage(c.PostForm("document"))
	view := settingsViewOf(doc)
	if pwd := c.PostForm("access-password"); pwd != "" {
		patched, hash, err := applyAccessPassword(doc, pwd)
		if err != nil {
			view.AccessError = err.Error()
		} else {
			doc = patched
			view = settingsViewOf(doc)
			view.AccessHash = hash
		}
	}
	// t 是片段模板的取词函数：片段不经 shell.Prepare，缺 t 时 Jet 把取词调用求值成空串。
	c.HTML(http.StatusOK, "fragments/settings_panel",
		gin.H{"Settings": view, "t": shell.TranslateFor(c)})
}

// applyAccessPassword 把明文访问密码变成 settings.access 里的 bcrypt 哈希。
//
// 边界（都是 fail closed，理由见内联注释）：
//   - 超长（> MaxAccessPasswordBytes）直接拒绝：bcrypt 只吃前 72 字节，
//     静默截断会让「设了 100 字符密码、输前 72 字符就进得去」；
//   - 密码为空由调用方判掉（空提交不是「清除密码」，清除走「公开」那档）；
//   - 服务端**总是**把 type 设成 password：用户没点过类型按钮时，
//     光有哈希而 type 为空会让产物按公开生成 —— 那是静默失效，
//     与「刚设了密码却什么都没生效」是同一类问题。
//
// 返回重写后的文档字节（其余字段原样透传）与本次算出的哈希。
func applyAccessPassword(doc json.RawMessage, password string) (json.RawMessage, string, error) {
	if len(password) > builder.MaxAccessPasswordBytes {
		return doc, "", fmt.Errorf("密码过长（上限 %d 字节）", builder.MaxAccessPasswordBytes)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return doc, "", errors.New("密码处理失败，请重试")
	}
	var parsed map[string]any
	if len(doc) > 0 {
		// 解析失败按「空文档」处理：面板对残缺 document 本来就是容错的
		// （settingsViewOf 也是这个口径），重设密码不该因为文档残缺而不可用。
		_ = json.Unmarshal(doc, &parsed)
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	settings, _ := parsed["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		parsed["settings"] = settings
	}
	access, _ := settings["access"].(map[string]any)
	if access == nil {
		access = map[string]any{}
		settings["access"] = access
	}
	access["type"] = builder.AccessPassword
	access["passwordHash"] = string(hash)
	out, merr := json.Marshal(parsed)
	if merr != nil {
		return doc, "", errors.New("密码处理失败，请重试")
	}
	return json.RawMessage(out), string(hash), nil
}

// workbenchSeoScorePanel 渲染 SEO 评分区片段（POST /workbench/seo-score-panel）。
func workbenchSeoScorePanel(c *gin.Context) {
	doc := json.RawMessage(c.PostForm("document"))
	c.HTML(http.StatusOK, "fragments/seo_score",
		gin.H{"Score": scoreViewOf(doc, c.PostForm("url"), requestScoreLang(c), shell.TranslateFor(c)),
			"t": shell.TranslateFor(c)})
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
			Access        struct {
				Type         string `json:"type"`
				PasswordHash string `json:"passwordHash"`
			} `json:"access"`
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
	// 访问权限类型归一：空 = 公开（面板里的「公开」按钮即空值，
	// 与 builder 侧「未设置 = 公开」的口径一致）。
	accessType := parsed.Settings.Access.Type
	if accessType == "" {
		accessType = "public"
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
		AccessType:        accessType,
		// 只回传「有没有设过」：哈希本身不进模板（见 settingsView 的说明）。
		AccessPasswordSet: strings.TrimSpace(parsed.Settings.Access.PasswordHash) != "",
	}
}

// scoreViewOf 计算并转换 SEO 评分（文档为空/评分失败时返回 OK=false，模板显示空态）。
func scoreViewOf(doc json.RawMessage, pageURL, lang string, trs ...func(key, fallback string) string) scoreView {
	tr := func(_, fallback string) string { return fallback }
	if len(trs) > 0 && trs[0] != nil {
		tr = trs[0]
	}
	if len(doc) == 0 {
		return scoreView{}
	}
	res, err := seoscore.ScoreDocument(doc, pageURL, lang)
	if err != nil || res == nil {
		return scoreView{}
	}
	sv := scoreView{OK: true, Total: res.Total, Grade: res.Grade}
	for _, sec := range res.Sections {
		item := scoreSectionView{
			Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
			ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
		}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, scoreIssueView{
				// 占位符是命名形态（{label}/{actual}/…）：词条可被运营在后台改，
				// 裸 % 或中英参数错位都会让 Sprintf 静默输出乱码。
				Text: i18n.FillTranslate(tr, projectenums.SEOScoreIssueFormat,
					"{label}：{actual}（基准 {benchmark}）→ {hint}",
					map[string]string{"label": ck.Label, "actual": ck.Actual,
						"benchmark": ck.Benchmark, "hint": ck.Hint}),
				Target: ck.Target,
			})
		}
		sv.Sections = append(sv.Sections, item)
	}
	// SERP 预览取页面设置里的标题/描述（空则占位）。
	sv.SerpTitle = settingsViewOf(doc).SEOTitle
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(projectenums.SEOScoreSerpTitleEmpty, "（未填写 SEO 标题）")
	} else {
		sv.SerpTitle = seoscore.TruncateDisplayWidth(sv.SerpTitle, 60)
	}
	sv.SerpDesc = settingsViewOf(doc).SEODescription
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(projectenums.SEOScoreSerpDescEmpty, "（未填写 SEO 描述）")
	}
	sv.SerpURL = pageURL
	if sv.SerpURL == "" {
		sv.SerpURL = "https://example.com/page"
	}
	return sv
}
