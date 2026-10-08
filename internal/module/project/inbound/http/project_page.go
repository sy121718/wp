package projecthttp

// 背景：workbench.js 的 renderSettingsPanel 用 150+ 行 DOM 代码拼「版心/SEO 字段 +
// segment 组 + 评分面板 + SERP 预览」。本文件把表单与评分都搬到服务端（Jet 片段），
// 客户端只保留「值变更 → 回写 doc.settings → 刷新画布/评分」。
//
// 端点：
//   POST /workbench/settings         → 表单片段（含评分容器占位）
//   POST /workbench/seo-score-panel  → 评分区片段（字段改动后局部刷新）

// （自 dashboard/inbound/http/site_settings_handle.go 与 site_locales_handle.go 迁入）。
//
// 页面：/admin/settings（GET + POST 保存）、/admin/settings/locales/rows（HTMX 行片段）、
// /admin/settings/locales/save（全量保存）。数据经 project 契约读写 projects.settings；
// 语言清单变更后的「全站标记待重建」经 page 契约编排（依赖方向 page → project，
// project 反向持有 page 契约由装配层注入）。

// 页面：/admin/themes（列表/新建/激活/删除）、/admin/theme（旧入口 301）。
// 主题换皮编排（AttachThemeToUnassigned / ReskinProjectForTheme / Refresh*）经 page
// 契约完成：语言清单归 project，「全站页面刷新」能力归 page，依赖方向 page → project，
// project 反向持有 page 契约是装配层注入的编排点（brief 明确保留）。

// （自 dashboard/inbound/http/global_handle.go 迁入）。
//
// 背景：workbench.js 的 renderGlobalPanel 用 100+ 行 DOM 代码拼 43 个主题字段
//（色板/排版/按钮/表面/动效）。本文件把字段定义与渲染搬到服务端；客户端只保留
// 「取色器增强 + 保存到 /admin/themes/settings/save」。
//
// 端点：POST /workbench/global（form: settings = themeSettings JSON、themeId）。

// 页面：/admin/themes/settings（GET 表单 + POST 保存）。
// block 契约仅做只读取数（页眉/页脚候选块列表）；block 的 stale 传播接线已由装配层接管，
// 本文件不编排。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"go_wp/internal/builder"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/project/dto"
	"go_wp/internal/module/project/enums"
	"go_wp/internal/module/sysconfig/contract"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/siteurl"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
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

// 站点设置页文案（i18n key，与 dashboard enums 迁移前同值）。
const (
	siteSettingsMsgTitle = "MsgSiteSettingsTitle"
	// siteSettingsLocalesInval 语言清单校验失败的就地提示（回渲染 + 回显用户输入，
	// 不经整页提示，所以不在 projectWriteTextKeys 里）。
	siteSettingsLocalesInval = "MsgSiteLocalesInvalid"
	// siteLocalesSavedKey / siteLocalesSavedFallback 语言清单保存成功的回执
	//（整页提示的 OK 分支；词条在迁移 187）。
	siteLocalesSavedKey      = "admin.settings.locales.saved"
	siteLocalesSavedFallback = "语言清单已保存。"
)

// urlPatternRow URL 规则的一行（一个实体类型）。
type urlPatternRow struct {
	Kind    string // 实体类型键（表单字段名 = urlPattern_<Kind>）
	Label   string // 展示名（"文章详情页"）
	Value   string // 当前配置值；空 = 未配置
	Default string // 默认模式（placeholder："留空 = 用这个"要看得见）
}

// buildURLPatternRows 组装 URL 规则编辑行（顺序取 siteurl.KnownKinds，稳定）。
func buildURLPatternRows(configured map[string]string) []urlPatternRow {
	rows := make([]urlPatternRow, 0, len(siteurl.KnownKinds))
	for _, k := range siteurl.KnownKinds {
		rows = append(rows, urlPatternRow{
			Kind:    k.Kind,
			Label:   k.Label,
			Value:   strings.TrimSpace(configured[k.Kind]),
			Default: siteurl.PatternOf(k.Kind, nil),
		})
	}
	return rows
}

// siteSettingsData 站点设置页数据。
type siteSettingsData struct {
	Title        string
	Menu         string
	Projects     []projectcontract.ProjectResp
	Selected     string // 当前选中工程 ID
	Name         string // 站点名称（工程名）
	SiteName     string // 站点显示名
	SiteDesc     string // 站点简介
	ContactEmail string // 联系邮箱
	// GA4MeasurementID 站点 GA4 测量 ID（构建期注入产物 head 的 gtag；空 = 不注入）。
	GA4MeasurementID string
	// SearchConsoleVerification GSC 站点验证 token（构建期注入验证 meta；空 = 不注入，SEO-009）。
	SearchConsoleVerification string
	// HeadScripts 站点自定义 Head 代码（构建期注入 </head> 之前；空 = 不注入，PIPE-8）。
	HeadScripts string
	// BodyScripts 站点自定义 Body 代码（构建期注入 </body> 之前；空 = 不注入，PIPE-8）。
	BodyScripts string
	// HeadScriptsError / BodyScriptsError 自定义代码校验失败的就地提示（空 = 无错误）。
	//
	// 与 Err 的分工：Err 是**装载失败**的降级提示（本页取数失败时给）；
	// 这两个是**就地回渲染**的提示 —— 与 LocaleError 同一路，因为这两个字段的内容
	// 是几百字节的脚本，整页提示一跳表单就空了，用户填的代码跟着丢（提示只带一句文案、
	// 不带表单内容）；而「用户的输入比错误文案贵」是写表单失败的既有口径。
	// 值携带 i18n key，模板经 .["t"](key, 中文兜底) 取词，缺词条也不会显示裸 key。
	HeadScriptsError string
	BodyScriptsError string
	// NotFoundHTML 站点自定义 404 页内容（发布时写到激活目录根的 404.html；
	// 空 = 不配置，且会删除既有 404.html，SEO-013）。
	NotFoundHTML string
	// ShippingBaseFeeYuan / ShippingFreeThresholdYuan 站点运费规则（**元**，表单口径）。
	//
	// 库内是分、表单是元：换算是这一对字段唯一的存在理由，且只在 fillProjectSettings
	//（分→元）与 SaveSiteSettings（元→分）两处发生，函数都是 projectdto 里的同一对
	//（整数拆分，不用 ParseFloat*100 —— 浮点乘偶尔差 1 分）。
	ShippingBaseFeeYuan       string
	ShippingFreeThresholdYuan string
	// URLPatterns URL 规则编辑行（各实体类型的详情页路径模式，WP 固定链接的等价物）。
	URLPatterns []urlPatternRow

	// Locales 站点语言清单编辑行（多语言 P3，project_locales）。
	Locales []localeRow
	// LocaleError 语言清单校验失败提示（空=无错误）。
	LocaleError string
	// IndexNowKey IndexNow 协议密钥（SEO-022）。
	IndexNowKey string
	// LangURLOffWarning 启用多语言但 url_mode=off 时的提示（I18N-016）。
	LangURLOffWarning bool
	// LangURLMode 语言 URL 方案（多语言开关）当前生效值：工程 settings 覆盖值优先，
	// 未配置时回显进程启动值 —— 面板展示的必须「就是现在跑着的那个」，
	// 否则作者看着 off 以为关了、实际进程在跑 default_plain。
	LangURLMode string
	// Err 本页**装载失败**的降级提示（空 = 无提示）。
	//
	// 与 LocaleError 的分工：LocaleError 是**语言清单**校验失败的就地提示（回渲染 + 回显用户输入，
	// 见 SaveSiteLocales）；Err 只由 buildSiteSettingsData 的取数失败填充。
	// 写动作的结论不再回显到本页（走 shell.RenderJump 渲染提示页，见 project_jump.go）。
	Err string
	// LangOptions 已收录语言（sys_dict type='language' 的启用项）——语言码 datalist 的
	// 唯一提示来源。与 project_locales.lang 同口径（完整语言码，如 zh-CN）。
	LangOptions []sysconfigcontract.DictOption
}

// templateMap 转 Jet 模板键 map（layout 以小写 title/menu 取值）。
func (d *siteSettingsData) templateMap() gin.H {
	return gin.H{
		"title":        d.Title,
		"menu":         d.Menu,
		"Projects":     d.Projects,
		"Selected":     d.Selected,
		"Name":         d.Name,
		"SiteName":     d.SiteName,
		"SiteDesc":     d.SiteDesc,
		"ContactEmail": d.ContactEmail,

		"GA4MeasurementID": d.GA4MeasurementID,

		"SearchConsoleVerification": d.SearchConsoleVerification,
		"HeadScripts":               d.HeadScripts,
		"BodyScripts":               d.BodyScripts,
		"HeadScriptsError":          d.HeadScriptsError,
		"BodyScriptsError":          d.BodyScriptsError,
		"NotFoundHTML":              d.NotFoundHTML,
		"ShippingBaseFeeYuan":       d.ShippingBaseFeeYuan,
		"ShippingFreeThresholdYuan": d.ShippingFreeThresholdYuan,
		"URLPatterns":               d.URLPatterns,
		"LangOptions":               d.LangOptions,
		"Locales":                   d.Locales,
		"LocaleError":               d.LocaleError,
		"IndexNowKey":               d.IndexNowKey,
		"LangURLOffWarning":         d.LangURLOffWarning,
		"LangURLMode":               d.LangURLMode,
		"Err":                       d.Err,
	}
}

// siteSettingsAdminHandle 站点设置页处理器。
type siteSettingsAdminHandle struct {
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
	// dict 字典只读口（语言码 datalist 的供数来源）；nil 时 datalist 为空。
	dict sysconfigcontract.DictReader
}

// NewSiteSettingsAdminHandle 测试与外部装配用的导出构造（三个契约均可 nil 降级）。
//
// dict 是**字典只读口**（sysconfig 契约）：给语言码的 datalist 供数。nil 时 datalist
// 为空 —— 用户仍可手输语言码（页面本来就是这个交互），只是失去了「已收录语言」的提示，
// 因此不算致命降级；但装配层必须传（见 assembly.go）。
func NewSiteSettingsAdminHandle(projects projectcontract.ProjectService, pages pagecontract.PageService, dict sysconfigcontract.DictReader) *siteSettingsAdminHandle {
	return &siteSettingsAdminHandle{projects: projects, pages: pages, dict: dict}
}

// SiteSettings 站点设置页（GET /admin/settings）。
func (h *siteSettingsAdminHandle) SiteSettings(c *gin.Context) {
	data := h.buildSiteSettingsData(c, strings.TrimSpace(c.Query("project")))
	c.HTML(http.StatusOK, "admin/project/settings", shell.Prepare(c, data.templateMap()))
}

// buildSiteSettingsData 组装站点设置页数据（工程列表 + 选中工程的基础信息与语言清单）。
// selected 为空时取第一个工程；工程不存在时只渲染工程选择器。
func (h *siteSettingsAdminHandle) buildSiteSettingsData(c *gin.Context, selected string) *siteSettingsData {
	data := &siteSettingsData{Title: siteSettingsMsgTitle, Menu: "settings"}
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		// 装载失败降级（与 theme 页同一判据）：空列表 + 归口提示，页面结构保留。
		//
		// 原先这里是「写 500 JSON 响应 → 再 c.HTML 渲染同一请求」：响应头已经发出，
		// 浏览器停在 JSON 上，后面那次渲染白做（Gin 会打 headers already written）。
		// 页面没被拿走才是重点 —— 运营还能换工程、走别的菜单，而不是对着一坨 JSON。
		data.Err = projectErrParam(c, "settings", projectenums.ErrProjectInternal, err)
		return data
	}
	data.Projects = projects
	data.Selected = selected
	if data.Selected == "" && len(projects) > 0 {
		data.Selected = projects[0].ID
	}
	// 语言码 datalist 的供数（字典只读口）：读失败**不**中断页面 —— 语言码输入框
	// 本来就允许手输，datalist 只是「已收录语言」的提示；把它升级成错误会让整页打不开。
	// 这里刻意不走缓存：站点设置页是后台页面路径（非访问面热路径），一次 100 行的
	// 字典查询可以接受；访问面的读取方走 pkg/i18n 的进程内缓存（见 RuntimeValues）。
	if h.dict != nil {
		if opts, derr := h.dict.ListDictOptions(c.Request.Context(), "language"); derr == nil {
			data.LangOptions = opts
		}
	}
	if data.Selected != "" {
		h.fillProjectSettings(c, data)
	}
	return data
}

// fillProjectSettings 填入选中工程的站点信息（名称 + settings 基础字段）。
func (h *siteSettingsAdminHandle) fillProjectSettings(c *gin.Context, data *siteSettingsData) {
	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: data.Selected})
	if err != nil || project == nil {
		logger.Scene("settings").With("project", data.Selected).Error(err, "读取站点工程失败")
		return
	}
	data.Name = project.Name
	// settings 为 json.RawMessage：解析基础字段回显；非对象或缺失字段按空处理。
	fields := projectcontract.ParseSiteSettings(project.Settings)
	data.SiteName = fields.SiteName
	data.SiteDesc = fields.SiteDesc
	data.ContactEmail = fields.ContactEmail
	data.GA4MeasurementID = fields.GA4MeasurementID
	data.SearchConsoleVerification = fields.SearchConsoleVerification
	// 自定义注入代码（PIPE-8）：按存储原文回显 —— 片段内部字节一个都不能改，
	// 否则作者下次保存时会把「设置页显示的那份」写回去，等于偷偷改了他的脚本。
	data.HeadScripts = fields.HeadScripts
	data.BodyScripts = fields.BodyScripts
	data.IndexNowKey = fields.IndexNowKey
	data.NotFoundHTML = fields.NotFoundHTML
	// 运费规则：库内分 → 表单元（0 回显为空串 = 未配置，与「留空即不收费」一致）。
	data.ShippingBaseFeeYuan = projectdto.FormatCentsAsYuan(fields.ShippingBaseFee)
	data.ShippingFreeThresholdYuan = projectdto.FormatCentsAsYuan(fields.ShippingFreeThreshold)
	// URL 规则：当前配置（可能为空）+ 默认模式（placeholder，"留空 = 用默认"要看得见）。
	data.URLPatterns = buildURLPatternRows(fields.URLPatterns)
	// 语言清单（project_locales）：站点「有哪几种语言」的唯一真源，与构建/路由同源。
	data.Locales = h.localeRowsOf(c, data.Selected)
	// 语言 URL 方案（多语言开关）：工程覆盖值优先；未配置时回显进程当前值 ——
	// 面板必须展示「现在真的在跑的那个」，否则开关的语义就是假的。
	data.LangURLMode = strings.TrimSpace(fields.LangURLMode)
	if data.LangURLMode == "" {
		// 未配置 → 回显**全局默认**（sys_config 的 i18n 组）：
		// 面板必须展示「这个工程现在真的在跑的那个」，否则开关的语义就是假的。
		data.LangURLMode = string(i18n.DefaultSiteLangURLMode())
	}
	data.LangURLOffWarning = langURLOffWarning(data.LangURLMode, data.Locales)
}

// langURLOffWarning 启用多种语言且该工程**生效**的方案为 off 时提示（I18N-016）。
//
// 入参是该工程生效的方案原文（工程值，未配置时调用方已填全局默认）：不再读进程级全局值 ——
// 那个值会让 A 工程设置页的提示按 B 工程的配置显示（多工程数据污染）。
func langURLOffWarning(mode string, rows []localeRow) bool {
	if strings.TrimSpace(mode) != string(i18n.SiteLangURLModeOff) {
		return false
	}
	enabled := 0
	for _, r := range rows {
		if r.Enabled {
			enabled++
		}
	}
	return enabled > 1
}

// SaveSiteSettings 保存站点设置（POST /admin/settings/save）。
//
// 两条口径：
//  1. **只动本页管的键**：在现有 settings 的键集合上合并（其它模块写进同一列的键原样保留），
//     整份覆盖会把别人写的配置悄悄删掉 —— 那种丢失在页面上看不出来，只在功能失效时才暴露。
//  2. **GA4 测量 ID 在保存时就校验**（与构建期注入同一判据）：不合法直接拒绝，
//     而不是存进去等构建期静默丢弃 —— 后者运维会以为统计代码已经装好了。
//
// 失败出口：整页提示（shell.RenderJump，见 project_jump.go）——**除了**自定义 Head / Body
// 代码非法那一条（就地回渲染回填用户输入，理由见下方注释）。
func (h *siteSettingsAdminHandle) SaveSiteSettings(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	// 缺参拆两条见 CreateTheme 的同类注释：工程缺失时连「回哪一页」都定不下来，
	// 只能回设置页入口（不带工程筛选）；名称缺失时工程是知道的，回该工程的设置页。
	if projectID == "" {
		projectJump(c, false, projectText(c, projectenums.ErrProjectRequired, "请先选择要操作的站点工程"),
			shell.BackPath(c, "/admin/settings"), siteSettingsBackText(c))
		return
	}
	back := siteSettingsBack(c)
	if name == "" {
		projectJump(c, false, projectText(c, projectenums.ErrSiteSettingsNameRequired, "工程与站点名称不能为空"),
			back, siteSettingsBackText(c))
		return
	}
	// 读取当前 settings，合并本页字段。
	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		// 工程不存在：原文只进日志（可能是别的实例刚删了它），对外一句受控文案。
		if err != nil {
			logger.Scene("settings").With("project", projectID).Error(err, "读取站点工程失败")
		}
		// 回设置页入口（不带工程筛选）：该工程已不存在，带着它的 id 回跳只会落到一个空表单。
		projectJump(c, false, projectText(c, projectenums.ErrProjectNotFound, "站点工程不存在"),
			shell.BackPath(c, "/admin/settings"), siteSettingsBackText(c))
		return
	}
	// URL 规则：逐实体类型读表单。留空 = 不配置该项（回落 siteurl 的默认模式）；
	// 非空则必须在保存时就校验 —— 存进去等构建期才发现问题的代价是"详情页路径莫名其妙"。
	patterns := map[string]string{}
	for _, k := range siteurl.KnownKinds {
		raw := strings.TrimSpace(c.PostForm("urlPattern_" + k.Kind))
		if raw == "" {
			continue
		}
		if perr := siteurl.ValidatePattern(raw); perr != nil {
			// 非法路径模式：原文（哪个实体、期望什么形状）只进日志，对外一句归口文案。
			projectJump(c, false, projectErrParam(c, "site_settings", projectenums.ErrProjectInternal, perr),
				back, siteSettingsBackText(c))
			return
		}
		patterns[k.Kind] = siteurl.Normalize(raw)
	}
	ga4ID := ""
	if raw := strings.TrimSpace(c.PostForm("ga4MeasurementId")); raw != "" {
		id, ok := builder.NormalizeGA4MeasurementID(raw)
		if !ok {
			projectJump(c, false, projectText(c, projectenums.ErrGA4IDInvalid, "GA4 测量 ID 格式不合法（形如 G-XXXXXXXXXX，只允许字母与数字）"),
				back, siteSettingsBackText(c))
			return
		}
		ga4ID = id
	}
	// GSC 验证 token 同样在保存时校验（与构建期注入同一判据）：不合法直接拒绝，
	// 而不是存进去等构建期静默丢弃 —— 后者运维会以为验证已经装上了。
	searchConsoleToken := ""
	if raw := strings.TrimSpace(c.PostForm("searchConsoleVerification")); raw != "" {
		token, ok := builder.NormalizeSearchConsoleVerification(raw)
		if !ok {
			projectJump(c, false, projectText(c, projectenums.ErrGSCVerificationInvalid, "Search Console 验证 token 格式不合法（base64url：字母、数字、- 与 _，8~128 位）"),
				back, siteSettingsBackText(c))
			return
		}
		searchConsoleToken = token
	}
	// 自定义注入代码（PIPE-8）：形状判据的唯一出口是 builder.NormalizeHeadScripts /
	// NormalizeBodyScripts（保存与注入共用同一份判据，否则会出现「后台存进去了、
	// 产物里却没有」这种最难排查的分歧）；非法（含结构性标签 / 超 16 KiB）时**不落库**。
	//
	// 出口用就地回渲染而不是整页提示（projectJump）：这两个字段装的是几百字节
	// 的脚本，整页提示一跳用户看到的是空表单 —— 他刚贴进去的代码就丢了，而提示只带
	// 一句文案、带不动表单内容。回渲染把输入原样带回，他只需改那一处（与语言清单保存
	// 失败同一条路，见下方的 SaveSiteLocales）。
	//
	// 附带一层：这两条提示**不进整页提示通道**，所以它们的 i18n key 不参与
	// projectWriteTextKeys 的「必须在迁移里登记」那条硬约束（本批不新增迁移）。
	rawHead := strings.TrimSpace(c.PostForm("headScripts"))
	rawBody := strings.TrimSpace(c.PostForm("bodyScripts"))
	headScripts, headOK := builder.NormalizeHeadScripts(rawHead)
	bodyScripts, bodyOK := builder.NormalizeBodyScripts(rawBody)
	if (rawHead != "" && !headOK) || (rawBody != "" && !bodyOK) {
		data := h.buildSiteSettingsData(c, projectID)
		data.HeadScripts = rawHead // 回显用户输入：校验失败清空表单是最伤的缺陷
		data.BodyScripts = rawBody
		if rawHead != "" && !headOK {
			data.HeadScriptsError = projectenums.SiteSettingsHeadScriptsInvalid
		}
		if rawBody != "" && !bodyOK {
			data.BodyScriptsError = projectenums.SiteSettingsBodyScriptsInvalid
		}
		c.HTML(http.StatusOK, "admin/project/settings", shell.Prepare(c, data.templateMap()))
		return
	}
	// 站点运费规则（站点级基础运费 + 满额免运费门槛）：库内单位是**分**，表单是**元**，
	// 换算与范围判据都在 projectdto（保存与读取同一份判据，见 shipping_policy_dto.go）。
	//
	// 非法值**不落库、不静默归零**：负运费等于倒贴钱（订单总额会被减掉一笔），
	// 而把它悄悄改成 0 会让运营以为自己配的运费生效了 —— 实际从没生效过，
	// 页面上看不出任何异常，只能等客户问「为什么没收运费」才发现。
	baseCents, baseErr := projectdto.ParseYuanToCents(c.PostForm("shippingBaseFee"))
	if baseErr != nil {
		projectJump(c, false, projectText(c, projectenums.ErrShippingBaseFeeInvalid, "基础运费金额不合法（负数 / 非数字 / 超上限）"),
			back, siteSettingsBackText(c))
		return
	}
	thresholdCents, thresholdErr := projectdto.ParseYuanToCents(c.PostForm("shippingFreeThreshold"))
	if thresholdErr != nil {
		projectJump(c, false, projectText(c, projectenums.ErrShippingFreeThresholdInvalid, "满额免运费门槛金额不合法（同上）"),
			back, siteSettingsBackText(c))
		return
	}
	// 组合判据（与读取侧同一个函数）：目前只有「非负 + 上限」，已由上面两次解析把住；
	// 这一步留着是为了让「什么算合法规则」只有一处定义 —— 将来加「门槛与基础运费的关系」
	// 这类跨字段规则时，不会漏掉保存侧这条链路。失败时提示落在门槛字段上：跨字段规则
	// 必然是「门槛与另一个字段的关系」，指到门槛最可定位。
	if _, nerr := projectdto.NormalizeShippingPolicy(baseCents, thresholdCents); nerr != nil {
		projectJump(c, false, projectText(c, projectenums.ErrShippingFreeThresholdInvalid, "满额免运费门槛金额不合法（同上）"),
			back, siteSettingsBackText(c))
		return
	}
	// 自定义 404 页（SEO-013）：只卡长度 —— 存的是一份完整 HTML 文档，没有可校验的
	// 「正确形状」（不同于 GA4 ID / GSC token）。超长会连同 projects.settings 整列一起
	// 被每次读设置解析，所以在入口拒绝，而不是存进去等发布时才发现。
	notFoundHTML := strings.TrimSpace(c.PostForm("notFoundHtml"))
	if len(notFoundHTML) > maxNotFoundHTMLLen {
		projectJump(c, false, projectText(c, projectenums.ErrNotFoundHTMLTooLong, "自定义 404 页内容过长（上限 32 KiB）"),
			back, siteSettingsBackText(c))
		return
	}
	settingsJSON, err := mergeSiteSettings(project.Settings, projectcontract.SiteSettings{
		SiteName:         strings.TrimSpace(c.PostForm("siteName")),
		SiteDesc:         strings.TrimSpace(c.PostForm("siteDesc")),
		ContactEmail:     strings.TrimSpace(c.PostForm("contactEmail")),
		GA4MeasurementID: ga4ID,
		// 站点验证 token：大小写敏感，原样保存（不做大小写归一化）。
		SearchConsoleVerification: searchConsoleToken,
		IndexNowKey:               strings.TrimSpace(c.PostForm("indexNowKey")),
		// 自定义注入代码：存归一化后的片段（只去两端空白，内部字节不动）——
		// 构建期注入读到的必须与设置页回显的是同一份字节。
		HeadScripts:  headScripts,
		BodyScripts:  bodyScripts,
		NotFoundHTML: notFoundHTML,
		URLPatterns:  patterns,
		// 运费规则（分）：与读取侧同源的两个键，换算已在上面完成。
		ShippingBaseFee:       baseCents,
		ShippingFreeThreshold: thresholdCents,
	})
	if err != nil {
		projectJump(c, false, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err),
			back, siteSettingsBackText(c))
		return
	}
	// 构建可见的字段**是否真的变了**：changed 决定保存后要不要标 stale。
	// 比较的是同一个 mergeSiteSettings 产出的规范化 JSON（键顺序稳定），
	// 因此「只点了一次保存、什么都没改」不会误判成变化 —— 那会把保存按钮变成一次全量重建。
	settingsChanged := siteSettingsDiffer(project.Settings, settingsJSON)
	if _, err := h.projects.Update(c.Request.Context(), &projectcontract.UpdateReq{
		ID: projectID, Name: name, Settings: settingsJSON,
	}); err != nil {
		projectJump(c, false, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err),
			back, siteSettingsBackText(c))
		return
	}
	// 保存成功之后才标记（失败路径不标：配置没落库，标了只会白重建一次）。
	h.markPagesStaleForSiteSettingsChange(c.Request.Context(), projectID, settingsChanged)
	projectJump(c, true, siteSettingsSavedText(c), back, siteSettingsBackText(c))
}

// siteSettingsDiffer 比较两份 settings 的**语义**（不看键顺序与空白）。
//
// 为什么不能逐字节比：raw 是从 PostgreSQL 的 jsonb 列读出来的（PG 会按自己的规则重排键、
// 去掉多余空白），merged 是 Go 的 json.Marshal 产物（按键排序）—— 同一份内容两边字节不同。
// 实测：逐字节比会把「什么都没改、只点了一次保存」判成变化，于是每次保存都全站重建
// （正是本函数要避免的事）。
//
// 解析失败按「变」处理（保守方向）：宁可多标一次 stale，也不要因为一份读不懂的旧值
// 而漏掉真正的内容变更。
func siteSettingsDiffer(before, after json.RawMessage) bool {
	var b, a map[string]any
	if err := json.Unmarshal(before, &b); err != nil {
		return true
	}
	if err := json.Unmarshal(after, &a); err != nil {
		return true
	}
	return !reflect.DeepEqual(b, a)
}

// markPagesStaleForSiteSettingsChange 站点设置变更后标记全站待重建。
//
// **为什么这些字段必须标 stale**：它们全部进产物字节 ——
//
//	· headScripts / bodyScripts → 注入 </head> 前 / </body> 前（builder/site_scripts.go、document.jet）
//	· searchConsoleVerification → head 里的验证 meta
//	· ga4MeasurementId → gtag 注入
//	· notFoundHtml → 自定义 404 页的响应体
//	· siteName / siteDesc → title 与 meta description
//	· urlPatterns → 详情页路径形态（站内链接与 canonical）
//
// 不标 stale 的表现是「改了统计脚本 / 换了 GSC token / 换了站点名 → 线上一个字节都不变」，
// 且没有任何报错 —— 只有手工全量重建才能生效（与 SaveSiteLocales 的清单变更是同一个失效模式）。
//
// **为什么用站点级标记而不是逐页反查**：影响面本来就是全站（没有哪一页不读这些键），
// 这不是「图省事退化成全站标记」—— 契约里那条「禁止退化为全站标记」针对的是
// **依赖键能精确匹配却图省事**的情形（MarkStaleByDependency 的注释）。站点设置目前没有
// 依赖键（DepKindSiteSetting 无发射点，见 pipeline/dependency.go 的说明），
// 走既有的站点级 stale 网是当下唯一正确且已有构建期登记的路径。
//
// 失败只记日志、不阻断保存：配置已落库，标记失败可由运维手动重建补上；
// 把保存回报成失败会让操作者重复提交（而第二次提交本身没有任何额外作用）。
func (h *siteSettingsAdminHandle) markPagesStaleForSiteSettingsChange(ctx context.Context, projectID string, changed bool) {
	if !changed || h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(ctx); err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "站点设置变更后标记全站待重建失败")
	}
}

// maxNotFoundHTMLLen 自定义 404 页内容上限（32 KiB）。
//
// 与模板 maxlength 同值（internal/templates/admin/settings.html）：模板那侧是输入体验，
// 这里才是判据（表单可以被绕过）。上限的理由是它整份存进 projects.settings，
// 而站点设置每条读取链路都要解析这一列。
const maxNotFoundHTMLLen = 32 * 1024

// mergeSiteSettings 把本页管理的站点设置字段合入现有 settings JSON：
// 其余键（本页不认识的）原样保留；字段为空串即删除该键（不落空值噪声，
// 也让「清空测量 ID = 停止注入统计代码」在存储层与实际行为一致）。
func mergeSiteSettings(raw json.RawMessage, fields projectcontract.SiteSettings) (json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
	}
	setMap := func(key string, value map[string]string) error {
		if len(value) == 0 {
			delete(obj, key)
			return nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		obj[key] = encoded
		return nil
	}
	setString := func(key, value string) error {
		if value == "" {
			delete(obj, key)
			return nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		obj[key] = encoded
		return nil
	}
	// setInt64 写整数键（运费金额，单位分）。
	//
	// 0 走**删除**分支：0 与「键缺失」在语义上等价（ParseSiteSettings 把缺失解析成 0），
	// 留一个 0 在 JSON 里只是噪声，而且会让「清空 = 关掉」在存储层看起来没生效。
	// 值直接用 strconv 拼 JSON 数字（不需要 json.Marshal 一层间接）。
	setInt64 := func(key string, value int64) error {
		if value == 0 {
			delete(obj, key)
			return nil
		}
		obj[key] = json.RawMessage(strconv.FormatInt(value, 10))
		return nil
	}
	// 运费规则：与读取侧（ShippingPolicyReader）同源的键名与判据。
	if err := setInt64("shippingBaseFee", fields.ShippingBaseFee); err != nil {
		return nil, err
	}
	if err := setInt64("shippingFreeThreshold", fields.ShippingFreeThreshold); err != nil {
		return nil, err
	}
	if err := setString("siteName", fields.SiteName); err != nil {
		return nil, err
	}
	if err := setString("siteDesc", fields.SiteDesc); err != nil {
		return nil, err
	}
	if err := setMap("urlPatterns", fields.URLPatterns); err != nil {
		return nil, err
	}
	if err := setString("contactEmail", fields.ContactEmail); err != nil {
		return nil, err
	}
	if err := setString("ga4MeasurementId", fields.GA4MeasurementID); err != nil {
		return nil, err
	}
	if err := setString("searchConsoleVerification", fields.SearchConsoleVerification); err != nil {
		return nil, err
	}
	if err := setString("indexNowKey", fields.IndexNowKey); err != nil {
		return nil, err
	}
	// 自定义注入代码（PIPE-8）：空串走 setString 的删除分支 —— 「清空 = 停止注入」
	// 在存储层与实际行为一致（产物里一个字节都不多），不留空值噪声。
	if err := setString("headScripts", fields.HeadScripts); err != nil {
		return nil, err
	}
	if err := setString("bodyScripts", fields.BodyScripts); err != nil {
		return nil, err
	}
	if err := setString("notFoundHtml", fields.NotFoundHTML); err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

// ---- 站点语言清单（多语言 P3） ----
//
// 入口：站点设置页的「语言」分组（/admin/settings）。语言清单按工程维度存储，
// 与站点名/简介同属一个工程设置面，故不新开独立页面（也避免多一个侧栏菜单与权限点）。
//
// 契约复用：读写一律经 project 模块既有契约 ListLocales / SaveLocales，
// 本模块不写第二套校验（「至少一种语言、至多一个默认且默认必须启用、语言码白名单」
// 在 project/service/locale_service.go 单点实现）。
//
// 交互（HTMX + Jet 服务端渲染，遵守既有后台页面写法）：
//   - 行片段 POST /admin/settings/locales/rows：增/删一行后由服务端重渲染行片段
//     （hx-target="#locale-rows"），未落库——保存仍由整表提交触发；
//   - 全量保存 POST /admin/settings/locales/save：普通表单 POST + 整页提示
//     （shell.RenderJump，见 project_jump.go），与同页「保存设置」一致。
//
// 行的身份用「提交顺序下标」而不是语言码：用户可以在表单里直接改语言码，
// 若用语言码做 radio/checkbox 的 value，改码后默认/启用勾选会静默丢失。

// localeRow 语言清单一行（页面编辑态）。
type localeRow struct {
	Lang      string
	IsDefault bool
	Enabled   bool
}

// localeRowsOf 读取工程语言清单并投影为编辑行（读失败按空清单处理，页面仍可用）。
func (h *siteSettingsAdminHandle) localeRowsOf(c *gin.Context, projectID string) []localeRow {
	if h.projects == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	rows, err := h.projects.ListLocales(c.Request.Context(), projectID)
	if err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "读取语言清单失败")
		return nil
	}
	out := make([]localeRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, localeRow{Lang: r.Lang, IsDefault: r.IsDefault, Enabled: r.Enabled})
	}
	return out
}

// parseLocaleRows 从表单解析语言行：
//
//	langs（按提交顺序的多值字段）+ defaultIndex（单个下标）+ enabledIndex（多值下标）。
//
// 下标越界/非法一律忽略，绝不 panic；语言码只做去空格，白名单校验交给 project 契约。
func parseLocaleRows(c *gin.Context) []localeRow {
	langs := c.PostFormArray("langs")
	if len(langs) == 0 {
		return nil
	}
	enabled := map[int]bool{}
	for _, raw := range c.PostFormArray("enabledIndex") {
		if i, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && i >= 0 {
			enabled[i] = true
		}
	}
	defaultIdx := -1
	if raw := strings.TrimSpace(c.PostForm("defaultIndex")); raw != "" {
		if i, err := strconv.Atoi(raw); err == nil && i >= 0 && i < len(langs) {
			defaultIdx = i
		}
	}
	rows := make([]localeRow, 0, len(langs))
	for i, lang := range langs {
		rows = append(rows, localeRow{
			Lang:      strings.TrimSpace(lang),
			IsDefault: i == defaultIdx,
			Enabled:   enabled[i],
		})
	}
	return rows
}

// LocaleRowsFragment POST /admin/settings/locales/rows（HTMX 片段）。
//
// action=add：把 newLang 追加为新行（默认不启用、不默认，避免误改默认语言）；
// action=remove：按 removeIndex 删除该行；删除默认语言行时把默认标记顺延给
// 第一个仍启用的行（保证片段里不会出现「无默认语言」的中间态）。
func (h *siteSettingsAdminHandle) LocaleRowsFragment(c *gin.Context) {
	rows := parseLocaleRows(c)
	switch strings.TrimSpace(c.PostForm("action")) {
	case "add":
		lang := strings.TrimSpace(c.PostForm("newLang"))
		rows = append(rows, localeRow{Lang: lang, Enabled: true})
	case "remove":
		if idx, err := strconv.Atoi(strings.TrimSpace(c.PostForm("removeIndex"))); err == nil && idx >= 0 && idx < len(rows) {
			removedDefault := rows[idx].IsDefault
			rows = append(rows[:idx:idx], rows[idx+1:]...)
			if removedDefault {
				for i := range rows {
					rows[i].IsDefault = false
				}
				for i := range rows {
					if rows[i].Enabled {
						rows[i].IsDefault = true
						break
					}
				}
			}
		}
	}
	c.HTML(http.StatusOK, "admin/partials/locale_rows", shell.Prepare(c, gin.H{"Locales": rows}))
}

// SaveSiteLocales POST /admin/settings/locales/save：全量保存站点语言清单。
//
// 校验（全部在 project.SaveLocales 内单点实现）：至少一种语言、至多一个默认语言
// 且默认语言必须启用、语言码白名单。校验失败时不落库，回渲染设置页并给出提示。
//
// 保存成功后按「清单内容是否变化」决定是否标记全站待重建（见
// markPagesStaleForLocaleChange）：切换器链接与 hreflang 是构建期写进产物字节的，
// 清单变了不重建，前台看不出变化（docs/06-D §15.9 遗留第 1 条）。
func (h *siteSettingsAdminHandle) SaveSiteLocales(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if projectID == "" {
		projectJump(c, false, projectText(c, projectenums.ErrProjectRequired, "请先选择要操作的站点工程"),
			shell.BackPath(c, "/admin/settings"), siteSettingsBackText(c))
		return
	}
	back := siteSettingsBack(c)
	// 变更前清单（project 契约的规范输出）：仅用于「是否变化」判定。
	// 读失败不阻断保存：before 为空切片，与保存结果比较必然判定为变化，保守触发重建。
	before, err := h.projects.ListLocales(c.Request.Context(), projectID)
	if err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "读取语言清单失败，按已变更处理")
	}
	rows := parseLocaleRows(c)
	req := &projectdto.LocalesSaveReq{ProjectID: projectID, Locales: make([]projectdto.LocaleItem, 0, len(rows))}
	for _, r := range rows {
		enabled := r.Enabled
		req.Locales = append(req.Locales, projectdto.LocaleItem{
			Lang: r.Lang, IsDefault: r.IsDefault, Enabled: &enabled,
		})
	}
	// 语言 URL 方案（多语言开关）与清单同表单保存：枚举在保存时就校验（与清单同一判据），
	// 非法值直接拒绝 —— 存进去等构建期才发现，代价是「访问路径莫名其妙」。
	modeRaw := strings.TrimSpace(c.PostForm("langURLMode"))
	modeChanged := false
	if modeRaw != "" {
		mode, merr := i18n.ParseSiteLangURLMode(modeRaw)
		if merr != nil {
			// 合法取值写在文案里，用户对着改即可。
			projectJump(c, false, projectText(c, projectenums.ErrLangURLModeInvalid, "语言 URL 方案取值非法（可选 off / default_plain / all_prefix）"),
				back, siteSettingsBackText(c))
			return
		}
		// 与**该工程**当前生效的方案比较：此前比的是进程级全局值，多工程下等于拿
		// 别的工程的设置当基准（判定本身就是错的）。未配置时基准是全局默认方案。
		current, cerr := h.projects.SiteLangURLMode(c.Request.Context(), projectID)
		if cerr != nil {
			// 读不到基准时按「已变更」处理：多标记一次全站待重建是可接受的代价，
			// 漏标记则会让「方案切换后产物路径没换」静默留在线上。
			logger.Scene("settings").With("project", projectID).
				Error(cerr, "读取当前语言 URL 方案失败，本次保存按已变更处理")
			current = ""
		}
		if strings.TrimSpace(current) == "" {
			current = string(i18n.DefaultSiteLangURLMode())
		}
		if mode != i18n.SiteLangURLMode(strings.TrimSpace(current)) {
			modeChanged = true
		}
		if err := h.saveLangURLMode(c.Request.Context(), projectID, mode); err != nil {
			projectJump(c, false, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err),
				back, siteSettingsBackText(c))
			return
		}
	}
	after, err := h.projects.SaveLocales(c.Request.Context(), req)
	if err != nil {
		// 就地回渲染：清单是一组可编辑行，整页提示一跳用户填的清单就丢了（回显便于就地修正）。
		logger.Scene("settings").With("project", projectID).Error(err, "保存语言清单失败")
		data := h.buildSiteSettingsData(c, projectID)
		data.Locales = rows // 回显用户输入，便于就地修正
		data.LocaleError = siteSettingsLocalesInval
		c.HTML(http.StatusOK, "admin/project/settings", shell.Prepare(c, data.templateMap()))
		return
	}
	h.markPagesStaleForLocaleChange(c.Request.Context(), projectID, before, after)
	if modeChanged {
		// 方案切换改变全部站内链接的形态（加不加语言前缀）：全站产物都必须重建，
		// 与清单变更同一张 stale 网 —— 失败只记日志（同 markPagesStaleForLocaleChange）。
		if h.pages != nil {
			if serr := h.pages.MarkStaleForI18n(c.Request.Context()); serr != nil {
				logger.Scene("settings").With("project", projectID).Error(serr, "语言 URL 方案变更后标记全站待重建失败")
			}
		}
	}
	projectJump(c, true, localesSavedText(c), back, siteSettingsBackText(c))
}

// saveLangURLMode 把语言 URL 方案持久化到工程 settings 并热更新进程值。
//
// saveLangURLMode 把语言 URL 方案持久化到**该工程的 settings**。
//
// 只落库、不写任何进程级状态：方案是工程级的，构建期按工程读（pipeline.SiteLangURLModeOf）。
// 此前这里有一步「热更新 pkg/i18n 的包级变量」，它同时造成两个缺陷 ——
// ① 多工程下 A 工程的保存会改变 B 工程的判定（数据污染）；
// ② 全局默认值改为由 sys_config 定时刷新后，这处热更新会被周期打回（保存最多生效 20s）。
// 现在保存后**立即生效**：下一个读点（构建 / 预览 / 设置页回显）直接读这份 settings。
func (h *siteSettingsAdminHandle) saveLangURLMode(ctx context.Context, projectID string, mode i18n.SiteLangURLMode) error {
	project, err := h.projects.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		if err == nil {
			err = fmt.Errorf("站点工程不存在: %s", projectID)
		}
		return err
	}
	// 定点合并而不是复用 mergeSiteSettings：后者面向整页表单，对零值字段的语义是
	// 「删除键」—— 单字段复用会把站点名 / 邮箱等既有配置整批误删。
	obj := map[string]json.RawMessage{}
	if len(project.Settings) > 0 {
		if err := json.Unmarshal(project.Settings, &obj); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(string(mode))
	if err != nil {
		return err
	}
	obj["langURLMode"] = encoded
	settingsJSON, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if _, err := h.projects.Update(ctx, &projectcontract.UpdateReq{ID: projectID, Name: project.Name, Settings: settingsJSON}); err != nil {
		return err
	}
	return nil
}

// markPagesStaleForLocaleChange 语言清单内容确实变化后，把全站页面标记为待重建。
//
// 语言清单归 project，「全站标记待重建」的能力归 page，且依赖方向是 page → project；
// 让 project 反向依赖 page 的实现会成环 —— 故经装配层注入的 page 契约编排。
//
// 触发条件：before/after 按「构建可见内容」比较不等（语言码集合与顺序、默认标记、
// 启用状态）。单语言站点原样再保存一次清单内容不变 → 不触发，避免无意义的全站重建。
//
// 失败只记日志：清单已落库（不因标记失败而回滚），下次保存会重新判定并再试。
func (h *siteSettingsAdminHandle) markPagesStaleForLocaleChange(ctx context.Context, projectID string, before, after []projectdto.LocaleResp) {
	if localesEqual(before, after) {
		return
	}
	if h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(ctx); err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "语言清单变更后标记全站待重建失败")
	}
}

// localesEqual 比较两次语言清单是否「构建可见」一致。
//
// 以 project.ListLocales 的规范输出为准（默认语言在前，其余按 sort_order、语言升序）：
// 该顺序正是构建期 EnabledLangs 的取用顺序，也就是产物里语言切换器与 hreflang 的顺序。
// 逐项比较语言码 + 默认标记 + 启用状态，任一不同即视为清单变化。
func localesEqual(a, b []projectdto.LocaleResp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Lang != b[i].Lang || a[i].IsDefault != b[i].IsDefault || a[i].Enabled != b[i].Enabled {
			return false
		}
	}
	return true
}

// 主题域页面文案（i18n key，与 dashboard enums 迁移前同值）。
//
// 归口文案（原 themePageMsgInternal = "MsgInternalError"）已随本批出口改造删除：
// 它只在 `c.String(500, …)` 里用过，而那 5 处现在都走 projectErrParam 的判定表
// （按域给 ErrThemeInternal / ErrProjectInternal），留着就是「两处归口文案」的死常量。
const (
	themePageMsgTitle = "MsgThemesTitle"
)

// themeAdminHandle 主题管理页处理器。
type themeAdminHandle struct {
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
	blocks   blockcontract.BlockService
}

// themeRow 主题列表行投影。
type themeRow struct {
	ID        string
	ProjectID string
	Name      string
	IsActive  bool
	CreatedAt string
	UpdatedAt string
}

// themeManageData 主题管理页数据。
type themeManageData struct {
	Title             string
	Menu              string
	Projects          []projectcontract.ProjectResp
	SelectedProjectID string
	Themes            []themeRow
	// Err 本页**装载失败**的降级提示（空 = 无提示）。
	//
	// 本页的写操作（新建 / 激活 / 删除）的结论不再回带 —— 走 shell.RenderJump 渲染
	// 整页提示（见 project_jump.go），所以提示条只由工程 / 主题列表取数失败填充。
	Err string
	// NoProjectEmpty 「还没有工程」空态：工程列表为空 **且** 本次装载没有失败。
	//
	// 不能只判 len(Projects)==0：装载失败时工程列表同样是空的，但那时该显示的是
	// 错误条而不是「请先创建一个站点工程」（用户明明有工程，是这一页没读出来）——
	// 两者的处置完全不同，所以判据在 handler 算好，模板只读一个布尔。
	NoProjectEmpty bool
}

// templateMap 转 Jet 模板键 map（layout 以小写 title/menu 取值）。
func (d *themeManageData) templateMap() gin.H {
	return gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProjectID,
		"Themes":          d.Themes,
		"Err":             d.Err,
		"NoProjectEmpty":  d.NoProjectEmpty,
	}
}

// renderThemeManage 渲染主题页 —— 三处出口（正常 / 工程装载失败 / 主题装载失败）共用，
// 保证 NoProjectEmpty 与 Err 的关系只有一处定义。
func (h *themeAdminHandle) renderThemeManage(c *gin.Context, data *themeManageData) {
	data.NoProjectEmpty = len(data.Projects) == 0 && data.Err == ""
	c.HTML(http.StatusOK, "admin/project/theme", shell.Prepare(c, data.templateMap()))
}

// ThemeManage 主题管理页：列出工程全部主题，支持新建/激活/删除。
// NewThemeAdminHandle 测试与外部装配用的导出构造（blocks 可空：仅主题设置页的候选块读取用）。
func NewThemeAdminHandle(projects projectcontract.ProjectService, pages pagecontract.PageService,
	blocks blockcontract.BlockService) *themeAdminHandle {
	return &themeAdminHandle{projects: projects, pages: pages, blocks: blocks}
}

func (h *themeAdminHandle) ThemeManage(c *gin.Context) {
	data := &themeManageData{
		Title: themePageMsgTitle,
		Menu:  "themes",
	}
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		// 装载失败不拿走整个页面（与 admin 六页同一判据）：空列表 + 归口提示，
		// 侧栏、页头、工程选择器全部保留 —— 运营看得出「是这一页没读出来」，
		// 而不是对着一块纯文本 / JSON 以为整个后台坏了。原文只进日志。
		data.Err = projectErrParam(c, "theme", projectenums.ErrThemeInternal, err)
		h.renderThemeManage(c, data)
		return
	}
	data.Projects = projects
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProjectID = sel
	} else if len(projects) > 0 {
		data.SelectedProjectID = projects[0].ID
	}
	if data.SelectedProjectID != "" {
		themes, terr := h.projects.ListThemes(c.Request.Context(), data.SelectedProjectID)
		if terr != nil {
			// 同上：主题列表读不出来时，页面仍要能操作（换工程、去别的菜单）。
			data.Err = projectErrParam(c, "theme", projectenums.ErrThemeInternal, terr)
			h.renderThemeManage(c, data)
			return
		}
		data.Themes = make([]themeRow, 0, len(themes))
		for _, t := range themes {
			data.Themes = append(data.Themes, themeRow{
				ID: t.ID, ProjectID: t.ProjectID, Name: t.Name, IsActive: t.IsActive,
				CreatedAt: t.CreatedAt.Time().Format("2006-01-02 15:04"),
				UpdatedAt: t.UpdatedAt.Time().Format("2006-01-02 15:04"),
			})
		}
	}
	h.renderThemeManage(c, data)
}

// CreateTheme 新建主题（POST /admin/themes/create）。
// 工程首个主题自动激活；创建后把工程内未挂主题的历史页面回填到新主题。
func (h *themeAdminHandle) CreateTheme(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	back := themesBack(c)
	// 缺参判断拆成两条，而不是合成一句「工程与主题名称不能为空」：合成句把
	// 「没选工程」与「没填名字」说成同一件事，而两者的修法完全不同
	// （前者是选择器 / URL 的问题，后者是输入框的问题）。两条都复用现成词条。
	if projectID == "" {
		projectJump(c, false, projectText(c, projectenums.ErrThemeProjectIDEmpty, "工程 ID 不能为空"),
			back, themesBackText(c))
		return
	}
	if name == "" {
		projectJump(c, false, projectText(c, projectenums.ErrThemeNameRequired, "主题名称不能为空"),
			back, themesBackText(c))
		return
	}
	theme, err := h.projects.CreateTheme(c.Request.Context(), &projectcontract.ThemeCreateReq{
		ProjectID: projectID, Name: name,
	})
	if err != nil {
		projectJump(c, false, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackText(c))
		return
	}
	// 回填：该工程 theme_id 为空的历史页面挂到新主题（失败不阻塞，可再次保存触发）。
	if err := h.pages.AttachThemeToUnassigned(c.Request.Context(), projectID, theme.ID); err != nil {
		logger.Scene("page").With("project", projectID).With("theme", theme.ID).Warn("回填未挂主题页面失败")
	}
	projectJump(c, true, themeCreatedText(c), back, themesBackText(c))
}

// ActivateTheme 激活主题（POST /admin/themes/activate）。
// 激活成功后对该主题所在工程的整站页面执行「换皮」：
//  1. 全部页面转挂到新激活主题（ReattachProjectPagesToTheme）；
//  2. 颜色/字体快照合入 settings.theme（RefreshThemeForTheme）；
//  3. 页眉/页脚绑定合入 settings.structure（RefreshStructureForTheme）；
//  4. 全部页面标记待重建（MarkStaleForTheme），下次构建即以新主题换皮。
//
// 已发布产物静态面不变，草稿重建即新主题；刷新失败不使激活回滚（激活已提交），仅记日志。
func (h *themeAdminHandle) ActivateTheme(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	back := themesBack(c)
	if id == "" {
		projectJump(c, false, projectText(c, projectenums.ErrThemeIDRequired, "缺少主题 id"),
			back, themesBackText(c))
		return
	}
	if err := h.projects.ActivateTheme(c.Request.Context(), &projectcontract.ThemeActivateReq{ID: id}); err != nil {
		projectJump(c, false, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackText(c))
		return
	}
	// 激活已提交：取新激活主题的 ProjectID 与 Settings，编排刷新整站页面。
	//
	// 下面三条是**部分成功**（激活落库了，整站换皮没做）：出口必须给可见的失败提示，
	// 不能让运营以为「激活了、页面也换好了」——原先这里是静默 303。
	theme, err := h.projects.GetTheme(c.Request.Context(), id)
	if err != nil || theme == nil {
		logger.Scene("theme").With("theme_id", id).Error(err, "激活后取主题失败，整站换皮未执行")
		projectJump(c, false, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackText(c))
		return
	}
	themeJSON, structureJSON, err := themeSnapshots(theme.Settings)
	if err != nil {
		logger.Scene("theme").With("theme_id", theme.ID).Error(err, "序列化主题快照失败")
		projectJump(c, false, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackText(c))
		return
	}
	if err := h.pages.ReskinProjectForTheme(c.Request.Context(), theme.ProjectID, theme.ID, themeJSON, structureJSON); err != nil {
		projectJump(c, false, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackText(c))
		return
	}
	projectJump(c, true, themeActivatedText(c), back, themesBackText(c))
}

// refreshThemePages 把主题设置应用到已挂该主题的页面（主题设置保存路径）：
// 刷新 settings.theme/structure 快照后标记待重建；失败返回非 0 状态码。
func (h *themeAdminHandle) refreshThemePages(c *gin.Context, themeID string, settingsJSON json.RawMessage) int {
	ctx := c.Request.Context()
	themeJSON, structureJSON, err := themeSnapshots(settingsJSON)
	if err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "序列化主题快照失败")
		return http.StatusInternalServerError
	}
	// 先刷新快照，再标记待重建。
	if err := h.pages.RefreshThemeForTheme(ctx, themeID, themeJSON); err != nil {
		return http.StatusInternalServerError
	}
	if err := h.pages.RefreshStructureForTheme(ctx, themeID, structureJSON); err != nil {
		return http.StatusInternalServerError
	}
	pageIDs, err := h.pages.MarkStaleForTheme(ctx, themeID)
	if err != nil {
		return http.StatusInternalServerError
	}
	// 标记完立刻重建（异步）：主题设置一改，挂它的页面产物就全部落后于设置 ——
	// 与块变更、组件更新同一条口径。同步做会把「保存主题设置」这个请求拖到
	// 整站构建完才返回（页面数量随站点规模线性增长）。
	if len(pageIDs) > 0 {
		batch := append([]string(nil), pageIDs...)
		go func() {
			// Background：重建要活过发起它的请求。
			if err := h.pages.RebuildStale(context.Background(), batch); err != nil {
				logger.Scene("theme").With("theme_id", themeID).With("count", len(batch)).
					Error(err, "主题变更后的自动重建失败（页面保持待重建，等下次触发）")
			}
		}()
	}
	return 0
}

// themeSnapshots 从主题设置构造页面文档快照：
//   - themeJSON = 完整 ThemeSettings（builder.ParseThemeSettings 校验后的原样 JSON，
//     不含 headerBlockId/footerBlockId），编译端 :root 变量；
//   - structureJSON = {headerBlockId, footerBlockId}（全局块槽位绑定）。
//
// 向后兼容：旧 themes.settings（5 色 colors + 顶层 fontFamily + headerBlockId/
// footerBlockId）仍可解析——ParseThemeSettings 能解析出 Colors.Primary 等，
// fontFamily 旧字段被忽略（新格式字体内聚在 typography.body.fontFamily）。
// settings 非法时返回可读错误。
func themeSnapshots(settings json.RawMessage) (themeJSON, structureJSON json.RawMessage, err error) {
	// 完整 ThemeSettings：解析 + 校验（含向后兼容旧 5 色格式）。
	ts, err := builder.ParseThemeSettings(settings)
	if err != nil {
		return nil, nil, err
	}
	themeJSON, err = json.Marshal(ts)
	if err != nil {
		return nil, nil, err
	}
	// 结构绑定：从顶层 headerBlockId/footerBlockId 取（不属于 ThemeSettings）。
	var s themeSettingsJSON
	if len(settings) > 0 {
		if err = json.Unmarshal(settings, &s); err != nil {
			return nil, nil, err
		}
	}
	fields := map[string]any{
		"headerBlockId": s.HeaderBlockID,
		"footerBlockId": s.FooterBlockID,
	}
	// 空 slots 不写：留一个 "slots":null 只是噪音，读取侧本来就按「空即无绑定」处理。
	if len(s.Slots) > 0 {
		fields["slots"] = s.Slots
	}
	structureJSON, err = json.Marshal(fields)
	if err != nil {
		return nil, nil, err
	}
	return themeJSON, structureJSON, nil
}

// DeleteTheme 删除主题（POST /admin/themes/delete；激活态由 service 拒绝）。
// 删除后该主题页面被 FK 置空（ON DELETE SET NULL），统一转挂到工程当前
// 激活主题，避免页面长期脱挂；工程无主题时保持 NULL（由下次建主题回填）。
func (h *themeAdminHandle) DeleteTheme(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	back := themesBack(c)
	if id == "" {
		projectJump(c, false, projectText(c, projectenums.ErrThemeIDRequired, "缺少主题 id"),
			back, themesBackText(c))
		return
	}
	projectID := ""
	if theme, err := h.projects.GetTheme(c.Request.Context(), id); err == nil && theme != nil {
		projectID = theme.ProjectID
	}
	if err := h.projects.DeleteTheme(c.Request.Context(), id); err != nil {
		projectJump(c, false, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackText(c))
		return
	}
	if projectID != "" {
		if active, err := h.projects.GetActiveTheme(c.Request.Context(), projectID); err == nil && active != nil {
			if err := h.pages.AttachThemeToUnassigned(c.Request.Context(), projectID, active.ID); err != nil {
				logger.Scene("page").With("project", projectID).With("theme", active.ID).Warn("删除主题后转挂页面失败")
			}
		}
	}
	projectJump(c, true, themeDeletedText(c), back, themesBackText(c))
}

// ThemeRedirect 旧入口 /admin/theme 301 到新主题管理页。
func (h *themeAdminHandle) ThemeRedirect(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/admin/themes")
}

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

// 主题设置页文案（i18n key，与 dashboard enums 迁移前同值）。
//
// 校验失败的文案已收进 projectenums.MsgThemeSettingsInvalid —— 它会进整页提示 / 错误槽，
// 而 projectWriteTextKeys 的迁移登记守卫要够得着它（本地常量让守卫只能靠抄字面量，抄错就静默失配）。
const (
	themeSettingsMsgTitle = "MsgThemeSettingsTitle"
)

// themeSettingsSavedTextKey / themeSettingsSavedTextFallback 保存成功回执（整页提示的
// OK 分支：key + 中文兜底成对，取词走 projectText）。
//
// 词条在 `sys_i18n`（迁移 451 已 seed 中英各一条）。此前这里只有一句中文常量、
// 以「值当 key」的方式取词 —— 库内没有中文 item_key，等于永远只显示中文。
const (
	themeSettingsSavedTextKey      = "admin.theme_settings.ok.saved"
	themeSettingsSavedTextFallback = "主题设置已保存，该主题下页面已标记待重建 —— 重新构建后新样式才会出现在访问面。"
)

// themeSettingsFieldInvalidText 字段级错误的一行红字（就近提示，模板渲染在控件下方）。
//
// 措辞只说「允许什么」而不复述白名单实现：`core.IsSafeCSSValue` 放行的是颜色、长度、
// 字号、关键字这类 CSS 值，用户要做的是「改成一个正常的值」，不是理解校验器。
const (
	themeSettingsFieldInvalidKey      = "admin.theme_settings.err.fieldInvalid"
	themeSettingsFieldInvalidFallback = "这个值不合法：只能填颜色（#3d444f）、尺寸（16px）这类 CSS 值。"
)

// themeSettingsData 单主题设置页数据（全局颜色/字体/页眉页脚块绑定）。
type themeSettingsData struct {
	Title     string
	Menu      string
	ThemeID   string
	ThemeName string
	ProjectID string
	// 以下 5 色 + 字体为旧字段，供 admin/theme_settings.html 回显（向后兼容）。
	PColor     string
	TColor     string
	BgColor    string
	SColor     string
	BdColor    string
	FontFamily string
	// ThemeSettingsJSON 完整 ThemeSettings JSON 字符串（colors 11 色 + typography +
	// button + surface + motion），供前端面板回显/扩展使用。
	ThemeSettingsJSON string
	// Groups 主题设置字段分组（与工作台全局设置面板共用同一份字段表）：
	// 颜色走取色器、固定档位走下拉、字体走可填可选的 datalist ——
	// 让主题设置页的控件类型由数据决定，而不是手写一堆文本框让用户猜格式。
	Groups []themeFieldGroupView
	// HeaderBlockID/FooterBlockID 全局页眉/页脚块绑定（编译期内联装配）。
	HeaderBlockID string
	FooterBlockID string
	// HeaderBlocks/FooterBlocks 该工程页眉/页脚候选块列表。
	HeaderBlocks []blockOption
	FooterBlocks []blockOption
	// AnnouncementBlocks 公告条候选块；AnnouncementBlockID 当前绑定（审计 VIS-012）。
	AnnouncementBlocks  []blockOption
	AnnouncementBlockID string
	// HeaderTemplateID / FooterTemplateID 页眉 / 页脚绑定的结构模板（可空 = 用块绑定）。
	//
	// 本页暂只做**原样回传**（渲染成隐藏域，保存时写回）：结构模板下拉与「生效」徽标
	// 属模板列表侧的后台改造，届时把隐藏域换成 select 即可，落库口径不用变。
	HeaderTemplateID string
	FooterTemplateID string
	// SlotTemplates 其余槽位的模板绑定（槽位名 → 模板 ID），同样原样回传。
	SlotTemplates map[string]string
	// HeaderTemplateOptions / FooterTemplateOptions 结构模板候选（页眉 / 页脚各一组）。
	//
	// 「不绑定」那一项由模板固定渲染（value 为空串），服务端只给真实候选 ——
	// 候选为空时下拉仍有一项可选，不会退化成「没有这个字段」（那才是清空绑定）。
	HeaderTemplateOptions []structureTemplateOptionView
	FooterTemplateOptions []structureTemplateOptionView
	// Err 保存失败的提示（空 = 无提示）。
	//
	// 注意失败路径**不跳页**：themeSettingsFailPage 就地重渲 200（提交值逐个回填 +
	// 出错字段标红 + 顶部提示）—— 整页提示一跳是一次新请求、没有 PostForm，
	// 52 个字段只能从库里旧值重建，用户填的东西必然全丢（admin-ui-logic §9 第 7 条）。
	// 本页原先对失败**没有任何出口** —— 失败是一块纯文本错误页。
	//
	// 保存成功的回执不再回显到本页（走 shell.RenderJump 渲染提示页，见 project_jump.go）。
	Err string
}

// structureTemplateOptionView 结构模板下拉项（selected 由服务端算好，前端不认识这组数据）。
type structureTemplateOptionView struct {
	ID       string
	Label    string
	Selected bool
}

// blockOption 页眉/页脚绑定候选下拉项。
type blockOption struct {
	ID   string
	Name string
	Kind string
}

// templateMap 转 Jet 模板键 map。
func (d *themeSettingsData) templateMap() gin.H {
	return gin.H{
		"title":                 d.Title,
		"menu":                  d.Menu,
		"ThemeID":               d.ThemeID,
		"ThemeName":             d.ThemeName,
		"ProjectID":             d.ProjectID,
		"PColor":                d.PColor,
		"TColor":                d.TColor,
		"BgColor":               d.BgColor,
		"SColor":                d.SColor,
		"BdColor":               d.BdColor,
		"FontFamily":            d.FontFamily,
		"ThemeSettings":         d.ThemeSettingsJSON,
		"Groups":                d.Groups,
		"HeaderBlock":           d.HeaderBlockID,
		"FooterBlock":           d.FooterBlockID,
		"HeaderBlocks":          d.HeaderBlocks,
		"FooterBlocks":          d.FooterBlocks,
		"AnnouncementBlocks":    d.AnnouncementBlocks,
		"AnnouncementBlock":     d.AnnouncementBlockID,
		"HeaderTemplate":        d.HeaderTemplateID,
		"FooterTemplate":        d.FooterTemplateID,
		"SlotTemplates":         d.SlotTemplates,
		"HeaderTemplateOptions": d.HeaderTemplateOptions,
		"FooterTemplateOptions": d.FooterTemplateOptions,
		"Err":                   d.Err,
	}
}

// themeSettingsJSON 与主题设置结构约定对齐（020_themes.sql / 021_blocks.sql 方案 C）：
// themes.settings 存储的 JSON = 完整 ThemeSettings（colors 11 色 + typography +
// button + surface + motion，经 builder.ParseThemeSettings 校验）+ 顶层
// headerBlockId/footerBlockId（全局块槽位绑定，不属于 ThemeSettings 模型，
// 但同存一份方便 themeSnapshots 分离快照）。
//
// 嵌入 builder.ThemeSettings 使 json.Marshal 扁平化输出完整主题字段；
// headerBlockId/footerBlockId 为顶层结构绑定。
type themeSettingsJSON struct {
	builder.ThemeSettings
	HeaderBlockID string `json:"headerBlockId,omitempty"`
	FooterBlockID string `json:"footerBlockId,omitempty"`
	// Slots 其余结构槽位的绑定（公告条 / 侧边栏等）：主题设置保存时与两个历史字段一起落库。
	Slots map[string]string `json:"slots,omitempty"`
	// HeaderTemplateID/FooterTemplateID/SlotTemplates 结构模板绑定（页眉 / 页脚的
	// 「多套存着、单套生效」之选），与块绑定同层、同一份快照里落库。
	//
	// **保存路径必须原样写回**：漏掉就是「在别处配好的结构模板，来这个页面保存一次
	// 主题设置就被清空」—— 站点上页眉悄悄回到旧块绑定，而页面上看不出任何异常。
	HeaderTemplateID string            `json:"headerTemplateId,omitempty"`
	FooterTemplateID string            `json:"footerTemplateId,omitempty"`
	SlotTemplates    map[string]string `json:"slotTemplates,omitempty"`
}

// ThemeSettings 单主题设置页。
//
// 缺 id / 主题不存在时渲染整页提示并回主题列表：本页**不知道自己该显示什么**（没有主题），
// 停在原地只能给一块错误页，而用户要的是回到能重新选主题的地方（列表页）。
func (h *themeAdminHandle) ThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.Query("id"))
	if themeID == "" {
		projectJump(c, false, projectText(c, projectenums.ErrThemeIDRequired, "缺少主题 id"),
			themesBack(c), themesBackText(c))
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		projectJump(c, false, projectText(c, projectenums.ErrThemeNotFound, "主题不存在"),
			themesBack(c), themesBackText(c))
		return
	}
	c.HTML(http.StatusOK, "admin/project/theme_settings", shell.Prepare(c, data.templateMap()))
}

// loadThemeSettings 组装单主题设置页数据；主题不存在返回 nil。
func (h *themeAdminHandle) loadThemeSettings(c *gin.Context, themeID string) *themeSettingsData {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	theme, err := h.projects.GetTheme(ctx, themeID)
	if err != nil || theme == nil {
		return nil
	}
	data := &themeSettingsData{
		Title: themeSettingsMsgTitle, Menu: "themes",
		ThemeID: theme.ID, ThemeName: theme.Name, ProjectID: theme.ProjectID,
	}
	// 完整 ThemeSettings：解析 + 校验（向后兼容旧 5 色 + 顶层 fontFamily 格式）。
	// 旧字段回显：PColor 等取自 ThemeColors；FontFamily 取自 typography.body.fontFamily
	// （旧顶层 fontFamily 被 ParseThemeSettings 忽略）。
	ts, perr := builder.ParseThemeSettings(theme.Settings)
	if perr != nil {
		logger.Scene("page").With("theme_id", themeID).Error(perr, "解析主题设置 JSON 失败")
	} else {
		data.PColor = ts.Colors.Primary
		data.TColor = ts.Colors.Text
		data.BgColor = ts.Colors.Background
		data.SColor = ts.Colors.Surface
		data.BdColor = ts.Colors.Border
		data.FontFamily = ts.Typography.Body.FontFamily
		if b, err := json.Marshal(ts); err == nil {
			data.ThemeSettingsJSON = string(b)
		}
	}
	// 结构绑定：顶层 headerBlockId/footerBlockId（不属于 ThemeSettings）。
	var s themeSettingsJSON
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &s); err != nil {
			logger.Scene("page").With("theme_id", themeID).Error(err, "解析主题结构绑定 JSON 失败")
		}
	}
	data.HeaderBlockID = s.HeaderBlockID
	data.FooterBlockID = s.FooterBlockID
	// 结构模板绑定：本页只做原样回传（隐藏域），不清空、不改写。
	data.HeaderTemplateID = s.HeaderTemplateID
	data.FooterTemplateID = s.FooterTemplateID
	data.SlotTemplates = s.SlotTemplates
	// 结构模板候选（页眉 / 页脚）：主题设置页的「选结构模板」下拉。取不到候选不是致命错误 ——
	// 下拉退化成只有「不绑定」一项，页面其余字段照常可保存（缺候选在启动日志里有 Warn）。
	if opts, oerr := h.projects.StructureTemplateOptions(ctx, theme.ProjectID); oerr != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(oerr, "读取结构模板候选失败（下拉候选为空，页面其余部分照常）")
	} else {
		for _, o := range opts {
			// 标签里带「当前生效」：多套模板并存时，下拉必须能看出哪一套是每次构建真正生效的那套，
			// 否则「选了另一套没生效」会被当成 bug（生效与否由模板列表的「设为生效」决定）。
			label := o.Name
			if o.IsDefault {
				label += tr(projectenums.ThemeSettingsStructureCurrent, "（当前生效）")
			}
			if o.EntityType == "footer" {
				data.FooterTemplateOptions = append(data.FooterTemplateOptions, structureTemplateOptionView{
					ID: o.ID, Label: label, Selected: o.ID == data.FooterTemplateID,
				})
				continue
			}
			data.HeaderTemplateOptions = append(data.HeaderTemplateOptions, structureTemplateOptionView{
				ID: o.ID, Label: label, Selected: o.ID == data.HeaderTemplateID,
			})
		}
	}
	// 其余槽位（目前是公告条）从 slots 映射里取：加新槽位时这里与模板各加一行。
	data.AnnouncementBlockID = s.Slots["announcement"]
	// 字段分组：以原始 JSON 为准（保真，不经过结构体丢掉历史/未来的键）。
	var rawSettings map[string]any
	if len(theme.Settings) > 0 {
		_ = json.Unmarshal(theme.Settings, &rawSettings)
	}
	data.Groups = buildThemeGroups(rawSettings, tr)
	// 页眉/页脚绑定候选：本工程的页眉/页脚类全局块。
	if blocks, err := h.blocks.List(ctx, &blockcontract.ListReq{ProjectID: theme.ProjectID}); err == nil {
		unset := tr(projectenums.ThemeSettingsBlockUnset, "（未设置）")
		data.HeaderBlocks = []blockOption{{ID: "", Name: unset}}
		data.FooterBlocks = []blockOption{{ID: "", Name: unset}}
		data.AnnouncementBlocks = []blockOption{{ID: "", Name: unset}}
		for _, b := range blocks {
			opt := blockOption{ID: b.ID, Name: b.Name, Kind: b.Kind}
			switch b.Kind {
			case "header":
				data.HeaderBlocks = append(data.HeaderBlocks, opt)
			case "footer":
				data.FooterBlocks = append(data.FooterBlocks, opt)
			case "announcement":
				data.AnnouncementBlocks = append(data.AnnouncementBlocks, opt)
			}
		}
	}
	return data
}

// SaveThemeSettings 保存单主题设置（POST /admin/themes/settings/save）：
// 写回 themes.settings（完整 ThemeSettings + 页眉/页脚块绑定）；主题设置批量合入
// 该主题下全部页面文档（settings.theme 快照），结构绑定合入 settings.structure；
// 保存后该主题下页面全部标待重建（新颜色/结构与块内容需重新构建生效）。
//
// PostForm 键名约定（扁平点分命名，前端 workbench.js 严格按此提交）：
//   - 颜色 11 个：colors.primary / colors.secondary / colors.accent / colors.success /
//     colors.warning / colors.danger / colors.text / colors.heading /
//     colors.background / colors.surface / colors.border
//   - 标题排版：typography.heading.color / typography.heading.weight（→ Heading.FontWeight）/
//     typography.heading.size（→ Heading.FontSize）/ typography.heading.spacing /
//     typography.heading.font（→ Heading.FontFamily）
//   - 正文排版：typography.body.color / typography.body.size（→ Body.FontSize）/
//     typography.body.line（→ Body.LineHeight）/ typography.body.font（→ Body.FontFamily）
//   - 链接：typography.link.color / typography.link.hover（→ Link.HoverColor）/
//     typography.link.underline
//   - 按钮：button.background / button.color / button.radius /
//     button.weight（→ Button.FontWeight）/ button.py（→ Button.PaddingY）/
//     button.px（→ Button.PaddingX）/ button.hoverBg（→ Button.HoverBackground）/
//     button.hoverColor
//   - 表面：surface.radius / surface.borderWidth / surface.borderColor / surface.shadow
//   - 动效：motion.duration（→ Motion.TransitionDuration）/ motion.easing /
//     motion.entrance（→ Motion.DefaultEntrance）
//   - 结构绑定（顶层）：headerBlockId / footerBlockId
func (h *themeAdminHandle) SaveThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.PostForm("id"))
	// 回跳地址由 shell.BackPath 从**本次请求的 query** 读回（表单 action 上带 ?id=<主题>），
	// 不读隐藏域里的整串 URL。id 缺失 / 主题不存在时回主题列表。
	if themeID == "" {
		projectJump(c, false, projectText(c, projectenums.ErrThemeIDRequired, "缺少主题 id"),
			themesBack(c), themesBackText(c))
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		projectJump(c, false, projectText(c, projectenums.ErrThemeNotFound, "主题不存在"),
			themesBack(c), themesBackText(c))
		return
	}
	// 本页地址（带主题 id）：成功 /「部分成功」整页提示以它为回跳基。
	//
	// 校验 / 落库失败**不走它** —— 那两条走 themeSettingsFailPage 的就地重渲：
	// 整页提示一跳是一次新请求、请求里没有 PostForm，52 个字段只能从库里重建，
	// 用户刚调完的一屏全没了。
	back := themeSettingsBack(c)
	// 从 PostForm（点分键名）组装完整 ThemeSettings；空值直接透传为字段零值，
	// 序列化时经 omitempty 省略（未设置字段不输出 CSS 变量，组件回退自身默认）。
	ts := &builder.ThemeSettings{
		// 图片管理：懒加载默认策略 + 骨架屏开关（组件级三态为「默认」时继承这里）。
		Images: builder.ThemeImages{
			LazyLoad: strings.TrimSpace(c.PostForm("images.lazyLoad")),
			Skeleton: strings.TrimSpace(c.PostForm("images.skeleton")) == "true",
		},
		Colors: builder.ThemeColors{
			Primary:    strings.TrimSpace(c.PostForm("colors.primary")),
			Secondary:  strings.TrimSpace(c.PostForm("colors.secondary")),
			Accent:     strings.TrimSpace(c.PostForm("colors.accent")),
			Success:    strings.TrimSpace(c.PostForm("colors.success")),
			Warning:    strings.TrimSpace(c.PostForm("colors.warning")),
			Danger:     strings.TrimSpace(c.PostForm("colors.danger")),
			Text:       strings.TrimSpace(c.PostForm("colors.text")),
			Heading:    strings.TrimSpace(c.PostForm("colors.heading")),
			Background: strings.TrimSpace(c.PostForm("colors.background")),
			Surface:    strings.TrimSpace(c.PostForm("colors.surface")),
			Border:     strings.TrimSpace(c.PostForm("colors.border")),
		},
		Typography: builder.ThemeTypography{
			Heading: builder.ThemeHeadingStyle{
				Color:      strings.TrimSpace(c.PostForm("typography.heading.color")),
				FontWeight: strings.TrimSpace(c.PostForm("typography.heading.weight")),
				FontSize:   strings.TrimSpace(c.PostForm("typography.heading.size")),
				Spacing:    strings.TrimSpace(c.PostForm("typography.heading.spacing")),
				FontFamily: strings.TrimSpace(c.PostForm("typography.heading.font")),
			},
			Body: builder.ThemeBodyStyle{
				Color:      strings.TrimSpace(c.PostForm("typography.body.color")),
				FontSize:   strings.TrimSpace(c.PostForm("typography.body.size")),
				LineHeight: strings.TrimSpace(c.PostForm("typography.body.line")),
				FontFamily: strings.TrimSpace(c.PostForm("typography.body.font")),
			},
			Link: builder.ThemeLinkStyle{
				Color:      strings.TrimSpace(c.PostForm("typography.link.color")),
				HoverColor: strings.TrimSpace(c.PostForm("typography.link.hover")),
				Underline:  strings.TrimSpace(c.PostForm("typography.link.underline")),
			},
		},
		Button: builder.ThemeButton{
			Background:      strings.TrimSpace(c.PostForm("button.background")),
			Color:           strings.TrimSpace(c.PostForm("button.color")),
			Radius:          strings.TrimSpace(c.PostForm("button.radius")),
			FontWeight:      strings.TrimSpace(c.PostForm("button.weight")),
			PaddingY:        strings.TrimSpace(c.PostForm("button.py")),
			PaddingX:        strings.TrimSpace(c.PostForm("button.px")),
			HoverBackground: strings.TrimSpace(c.PostForm("button.hoverBg")),
			HoverColor:      strings.TrimSpace(c.PostForm("button.hoverColor")),
			BorderWidth:     strings.TrimSpace(c.PostForm("button.borderWidth")),
			BorderStyle:     strings.TrimSpace(c.PostForm("button.borderStyle")),
			BorderColor:     strings.TrimSpace(c.PostForm("button.borderColor")),
			Shadow:          strings.TrimSpace(c.PostForm("button.shadow")),
		},
		Surface: builder.ThemeSurface{
			Radius:      strings.TrimSpace(c.PostForm("surface.radius")),
			BorderWidth: strings.TrimSpace(c.PostForm("surface.borderWidth")),
			BorderColor: strings.TrimSpace(c.PostForm("surface.borderColor")),
			Shadow:      strings.TrimSpace(c.PostForm("surface.shadow")),
		},
		Motion: builder.ThemeMotion{
			TransitionDuration: strings.TrimSpace(c.PostForm("motion.duration")),
			Easing:             strings.TrimSpace(c.PostForm("motion.easing")),
			DefaultEntrance:    strings.TrimSpace(c.PostForm("motion.entrance")),
		},
	}
	// 完整存储 JSON = 校验后的 ThemeSettings + 顶层结构绑定。
	settingsJSON, err := json.Marshal(themeSettingsJSON{
		ThemeSettings: *ts,
		HeaderBlockID: strings.TrimSpace(c.PostForm("headerBlockId")),
		FooterBlockID: strings.TrimSpace(c.PostForm("footerBlockId")),
		Slots:         themeSlotFormValues(c),
		// 结构模板绑定：见 structureTemplateFormValue —— 带哨兵域时按提交值写回（空值 = 主动解绑），
		// 不带时保持已存值（旧表单/缺字段不能让保存一次颜色就把页眉模板清空）。
		HeaderTemplateID: structureTemplateFormValue(c, "headerTemplateId", data.HeaderTemplateID),
		FooterTemplateID: structureTemplateFormValue(c, "footerTemplateId", data.FooterTemplateID),
		SlotTemplates:    themeSlotTemplateFormValues(c, data.SlotTemplates),
	})
	if err != nil {
		// 组装 JSON 失败（几乎不可达：字段全是字符串）—— 同样就地重渲，保住已经填好的 52 个值。
		themeSettingsFailPage(c, data, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err), nil, nil)
		return
	}
	// ParseThemeSettings 校验（IsSafeCSSValue 白名单，防 CSS 注入）；非法就地重渲并给提示。
	if _, verr := builder.ParseThemeSettings(settingsJSON); verr != nil {
		// CSS 值白名单不通过：原文（哪个字段、期望什么形状）只进日志，对外一句归口文案 +
		// 出错字段的就近红字（`themeSettingsInvalidField` 按出错值定位字段）。
		logger.Scene("theme").With("theme_id", themeID).Error(verr, "主题设置校验失败")
		themeSettingsFailPage(c, data, projectText(c, projectenums.MsgThemeSettingsInvalid, "主题设置不合法"), verr, settingsJSON)
		return
	}
	if _, err := h.projects.UpdateTheme(c.Request.Context(), &projectcontract.ThemeUpdateReq{
		ID: themeID, Name: data.ThemeName, Settings: settingsJSON,
	}); err != nil {
		themeSettingsFailPage(c, data, projectErrParam(c, "theme", projectenums.ErrThemeInternal, err), nil, settingsJSON)
		return
	}
	// 颜色/字体快照合入 settings.theme + 页眉/页脚绑定合入 settings.structure，
	// 再标记待重建（新颜色/结构与块内容需重新构建生效）。
	if code := h.refreshThemePages(c, themeID, settingsJSON); code != 0 {
		// 部分成功：设置**已经落库**，失败的只是「合入页面文档 + 标记待重建」这一步。
		// 提示必须说清这层区别，否则用户以为白填了一遍又填一次。
		projectJump(c, false, projectText(c, projectenums.MsgThemeSettingsRefreshFailed,
			"主题设置已保存，但整站页面刷新失败 —— 稍后在页面列表里重建即可，不必重填。"),
			back, themeSettingsBackText(c))
		return
	}
	// 成功：整页提示（1 秒后回本页）。保存的后果是「该主题下全部页面标记待重建」，
	// 页面与保存前逐字相同，用户需要一个明确的确认。
	projectJump(c, true, themeSettingsSavedText(c), back, themeSettingsBackText(c))
}

// themeSettingsFailPage 保存失败的**统一出口**：就地重渲本页（200 + 回填 + 错误槽 + 出错字段标红）。
//
// 为什么不是整页提示（本模块其它写操作的形态，见 project_jump.go）：
// 那个形态**回不到「输入还在」**。整页提示是一次新请求，请求里没有 PostForm，模板里
// 52 个字段的 value 只能来自库里那份旧 JSON（内嵌 `theme-settings-json` + 前端回显脚本），
// 于是用户刚调好的 11 个颜色、字体、结构绑定一次性回到旧值 —— 页面上「看起来什么都没发生」。
// 就地重渲把提交值直接渲回表单，无 JS、无暂存、无新基建。
//
// 代价是「刷新会重放这次提交」，而本页的写操作是幂等的（同值写回 + 重新标 stale），
// 重放无害；真正的失败态（校验不过）重放也只是再看到同样的提示。
//
// 另一条正路（htmx 档 200 + 片段自身）需要把表单抽成独立片段模板
// （`internal/templates/admin/project/theme_settings_form.html`），越出本批文件清单。
//
// echoSettings 是本次提交的 JSON（可为空 = 组装 JSON 那一步就失败了，保持库里原值）：
// 前端回显脚本按点分键从内嵌 JSON 取 value 覆盖各输入框，留着旧 JSON 会把刚回填好的
// 字段再改回旧值 —— 那正是「输入全丢」的现场，所以这里必须换成提交值。
// json.Marshal 默认转义 HTML（`<` → `\u003c`），`</script>` 逃逸不成立。
func themeSettingsFailPage(c *gin.Context, data *themeSettingsData, text string, fieldErr error, echoSettings json.RawMessage) {
	data.Err = text
	if len(echoSettings) > 0 {
		data.ThemeSettingsJSON = string(echoSettings)
	}
	themeSettingsEchoForm(c, data)
	if name, _, _ := themeSettingsInvalidField(data.Groups, fieldErr); name != "" {
		themeSettingsMarkInvalid(data.Groups, name,
			shell.TranslateFor(c)(themeSettingsFieldInvalidKey, themeSettingsFieldInvalidFallback))
	}
	c.HTML(http.StatusOK, "admin/project/theme_settings", shell.Prepare(c, data.templateMap()))
}

// themeSettingsEchoForm 失败回填：把本次提交的表单值覆盖到页面数据上（「输入不丢」的全部实现）。
//
// 覆盖三处，缺一处就是「保存一次颜色，把别处配好的东西丢了」：
//   - Groups 各字段的回显值（含 select 的 selected 重算）；
//   - 页眉 / 页脚 / 公告条块绑定（三个 select，服务端算 selected）；
//   - 结构模板绑定与其余槽位（哨兵语义见 structureTemplateFormValue，
//     沿用「带哨兵按提交值、不带保持原值」，回填与落库口径因此完全一致）。
func themeSettingsEchoForm(c *gin.Context, data *themeSettingsData) {
	_ = c.Request.ParseForm()
	form := c.Request.PostForm
	data.Groups = themeSettingsEchoGroups(form, data.Groups)
	if v, ok := themeSettingsFormValue(form, "headerBlockId"); ok {
		data.HeaderBlockID = v
	}
	if v, ok := themeSettingsFormValue(form, "footerBlockId"); ok {
		data.FooterBlockID = v
	}
	if v, ok := themeSettingsFormValue(form, "slots.announcement"); ok {
		data.AnnouncementBlockID = v
	}
	data.HeaderTemplateID = structureTemplateFormValue(c, "headerTemplateId", data.HeaderTemplateID)
	data.FooterTemplateID = structureTemplateFormValue(c, "footerTemplateId", data.FooterTemplateID)
	themeSettingsMarkTemplateSelected(data.HeaderTemplateOptions, data.HeaderTemplateID)
	themeSettingsMarkTemplateSelected(data.FooterTemplateOptions, data.FooterTemplateID)
	if slots := themeSlotTemplateFormValues(c, data.SlotTemplates); slots != nil {
		data.SlotTemplates = slots
	}
}

// themeSettingsEchoGroups 用提交值覆盖字段回显值；select 的选中项按值重算（与 buildThemeGroups 同口径）。
//
// 判据是「表单里有没有这个键」而不是「值是不是空串」：用户把颜色**清空**（跟随内置默认）
// 也是一次真实输入，回落成库里的旧颜色等于替他改回去。
func themeSettingsEchoGroups(form url.Values, groups []themeFieldGroupView) []themeFieldGroupView {
	for gi := range groups {
		for fi := range groups[gi].Fields {
			f := &groups[gi].Fields[fi]
			values, ok := form[f.Name]
			if !ok || len(values) == 0 {
				continue
			}
			f.Value = values[0]
			for oi := range f.Options {
				f.Options[oi].Selected = f.Options[oi].Value == f.Value
			}
		}
	}
	return groups
}

// themeSettingsFormValue 取表单里某个键的第一个值（缺键返回 false —— 与「提交了空串」区分）。
func themeSettingsFormValue(form url.Values, key string) (string, bool) {
	values, ok := form[key]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// themeSettingsMarkTemplateSelected 按当前绑定值重算结构模板下拉的 selected。
//
// 模板用 `<option … {{if o.Selected}}selected{{end}}>` 渲染，Selected 是服务端在
// loadThemeSettings 里按**库里**的绑定算好的；回填后绑定值变了，必须一起重算 ——
// 否则用户在下拉里刚选的模板会被渲染回旧的那一项。
func themeSettingsMarkTemplateSelected(options []structureTemplateOptionView, current string) {
	for i := range options {
		options[i].Selected = options[i].ID == current
	}
}

// themeSettingsInvalidField 从校验错误里定位出错的字段（返回提交键名 / 字段标签 / 出错的值）。
//
// `builder.ValidateThemeSettings` 的错误形态是 `主题设置 主色 值非法: "not-a-css-value;evil"`：
// 前半段是**中文标签**，但它与字段表的 Label 并不同名（「主色」对「主色」是巧合，
// 「按钮悬停背景」对「悬停背景」、「背景色」对「页面背景」都对不上），而且校验是 map 遍历
// （一次只报一个、顺序不保证）。所以按**值**定位：%q 出来的那段就是用户提交的原文，
// 在字段表里找值等于它的字段。两个字段填了同一个非法值时取第一个 —— 就近提示指到其中之一，
// 顶部归口提示（「主题设置不合法」）本来也不声称只有一个字段有问题。
func themeSettingsInvalidField(groups []themeFieldGroupView, err error) (name, label, value string) {
	if err == nil {
		return "", "", ""
	}
	bad, ok := themeSettingsQuotedValue(err.Error())
	if !ok {
		return "", "", ""
	}
	for _, g := range groups {
		for _, f := range g.Fields {
			// 校验发生在 TrimSpace 之后（SaveThemeSettings 提交值一律 trim），
			// 回填的却是用户原文（带空格也照原样显示），所以两边都比一次。
			if f.Value == bad || strings.TrimSpace(f.Value) == bad {
				return f.Name, f.Label, bad
			}
		}
	}
	return "", "", bad
}

// themeSettingsQuotedValue 取错误文本里最后一个 %q 引号串的内容。
//
// 失败一律返回 false（不定位，只留顶部提示）—— 这是一条**纯增强**路径，
// 认不出形态时宁可少一条就近红字，也不要指错字段。
func themeSettingsQuotedValue(msg string) (string, bool) {
	i := strings.LastIndex(msg, `: "`)
	if i < 0 || !strings.HasSuffix(msg, `"`) || len(msg)-i < 4 {
		return "", false
	}
	v, err := strconv.Unquote(msg[i+2:])
	if err != nil {
		return "", false
	}
	return v, true
}

// themeSettingsMarkInvalid 给定位到的字段打上错误态（模板据此渲染 aria-invalid 与就近红字）。
func themeSettingsMarkInvalid(groups []themeFieldGroupView, name, text string) {
	if name == "" {
		return
	}
	for gi := range groups {
		for fi := range groups[gi].Fields {
			if groups[gi].Fields[fi].Name == name {
				groups[gi].Fields[fi].Invalid = true
				groups[gi].Fields[fi].Error = text
				return
			}
		}
	}
}

// 「没提交这个字段」与「提交了空值」必须分开：前者是旧表单 / 缺字段（保持原值 ——
// 不能让「保存一次颜色」把配好的页眉模板清空），后者是用户在下拉里选了「不绑定」
// （必须真的解绑）。判据是表单里的哨兵域 structureTemplateFields：新版表单渲染了
// 结构模板区块就带它。
//
// 为什么不用「表单里有 headerTemplateId 键」当判据：多套模板并存时下拉本身就带空值项，
// select 永远会提交这个键；而旧版表单根本没有这个键 —— 两件事用一个哨兵说清楚比猜更可靠。
func structureTemplateFormValue(c *gin.Context, key, current string) string {
	if strings.TrimSpace(c.PostForm("structureTemplateFields")) != "1" {
		return firstNonEmpty(strings.TrimSpace(c.PostForm(key)), current)
	}
	return strings.TrimSpace(c.PostForm(key))
}

// firstNonEmpty 取第一个非空串（表单没提交时回落到已存值，避免保存即清空）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// themeSlotTemplateFormValues 收集表单里的槽位模板绑定（点分键 slotTemplates.<槽位名>）。
//
// 与 themeSlotFormValues 同构：前缀扫描，将来加槽位不必改这里。表单没有提交任何
// slotTemplates 键时**原样返回已存值**（同上：不丢数据优先）。
func themeSlotTemplateFormValues(c *gin.Context, current map[string]string) map[string]string {
	const prefix = "slotTemplates."
	_ = c.Request.ParseForm()
	out := map[string]string{}
	for key, values := range c.Request.PostForm {
		if !strings.HasPrefix(key, prefix) || len(values) == 0 {
			continue
		}
		slot := strings.TrimSpace(strings.TrimPrefix(key, prefix))
		if id := strings.TrimSpace(values[0]); slot != "" && id != "" {
			out[slot] = id
		}
	}
	if len(out) == 0 {
		return current
	}
	return out
}

// themeSlotFormValues 收集表单里的结构槽位绑定（点分键 slots.<槽位名>）。
//
// 用前缀扫描而不是逐个 c.PostForm("slots.announcement")：主题设置页加一个槽位只需
// 多一个 select，这里不必改 —— 而漏改一处就是「配了不生效」，且页面上看不出任何异常。
func themeSlotFormValues(c *gin.Context) map[string]string {
	const prefix = "slots."
	_ = c.Request.ParseForm()
	out := map[string]string{}
	for key, values := range c.Request.PostForm {
		if !strings.HasPrefix(key, prefix) || len(values) == 0 {
			continue
		}
		slot := strings.TrimSpace(strings.TrimPrefix(key, prefix))
		if id := strings.TrimSpace(values[0]); slot != "" && id != "" {
			out[slot] = id
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
