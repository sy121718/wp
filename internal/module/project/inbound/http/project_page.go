package projecthttp

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/pathkit"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/siteurl"
	"go_wp/pkg/backpath"
	"go_wp/pkg/binding"
	"go_wp/pkg/builder"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	seoscore "go_wp/pkg/seo"
	"go_wp/pkg/seo/scoreview"
	pageview "go_wp/pkg/view"
)

func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

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

	ThemeOverrides []themeOverrideFieldView

	AccessType string

	AccessPasswordSet bool

	AccessHash string

	AccessError string
}

type themeOverrideFieldView struct {
	Label string

	Path string

	Value string
}

var themeOverrideKinds = map[string]bool{"color": true}

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

type scoreView = scoreview.View

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

	pageview.Render(c, &projectdto.SettingsPanelView{Settings: view})
}

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

func workbenchSeoScorePanel(c *gin.Context) {
	doc := json.RawMessage(c.PostForm("document"))
	pageview.Render(c, &projectdto.SeoScorePanelView{Score: scoreViewOf(doc, c.PostForm("url"), requestScoreLang(c), projectTr(c))})
}

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

		AccessPasswordSet: strings.TrimSpace(parsed.Settings.Access.PasswordHash) != "",
	}
}

func scoreViewOf(doc json.RawMessage, pageURL, lang string, _ ...func(key, fallback string) string) scoreView {
	if len(doc) == 0 {
		return scoreView{}
	}
	res, err := seoscore.ScoreDocument(doc, pageURL, lang)
	if err != nil || res == nil {
		return scoreView{}
	}
	sv := scoreview.Of(res)

	settings := settingsViewOf(doc)
	sv.SerpTitle = settings.SEOTitle
	sv.HasSerpTitle = strings.TrimSpace(settings.SEOTitle) != ""
	sv.SerpDesc = settings.SEODescription
	sv.HasSerpDesc = strings.TrimSpace(settings.SEODescription) != ""
	sv.SerpURL = pageURL
	if strings.TrimSpace(pageURL) == "" {
		sv.SerpURL = "https://example.com/page"
	}
	sv.HasSerpURL = true
	return sv
}

const (
	siteSettingsMsgTitle = "MsgSiteSettingsTitle"

	siteSettingsLocalesInval = "MsgSiteLocalesInvalid"

	siteLocalesSavedFallback = "语言清单已保存。"
)

type urlPatternRow struct {
	Kind    string
	Label   string
	Value   string
	Default string
}

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

type siteSettingsData struct {
	Title        string
	Menu         string
	Projects     []projectcontract.ProjectResp
	Selected     string
	Name         string
	SiteName     string
	SiteDesc     string
	ContactEmail string

	GA4MeasurementID string

	SearchConsoleVerification string

	HeadScripts string

	BodyScripts string

	HeadScriptsError string
	BodyScriptsError string

	NotFoundHTML string

	ShippingBaseFeeYuan       string
	ShippingFreeThresholdYuan string

	URLPatterns []urlPatternRow

	Locales []localeRow

	LocaleError string

	IndexNowKey string

	LangURLOffWarning bool

	LangURLMode string

	Err projectMsg

	LangOptions []sysconfigcontract.DictOption
}

type siteSettingsAdminHandle struct {
	projects projectcontract.ProjectService
	pages    pagecontract.PageService

	dict sysconfigcontract.DictReader
}

func NewSiteSettingsAdminHandle(projects projectcontract.ProjectService, pages pagecontract.PageService, dict sysconfigcontract.DictReader) *siteSettingsAdminHandle {
	return &siteSettingsAdminHandle{projects: projects, pages: pages, dict: dict}
}

func (h *siteSettingsAdminHandle) SiteSettings(c *gin.Context) {
	data := h.buildSiteSettingsData(c, strings.TrimSpace(c.Query("project")))
	pageview.Render(c, data)
}

func (h *siteSettingsAdminHandle) buildSiteSettingsData(c *gin.Context, selected string) *siteSettingsData {
	data := &siteSettingsData{Title: siteSettingsMsgTitle, Menu: "settings"}
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {

		data.Err = projectErrMsg(c, "settings", projectenums.ErrProjectInternal, err)
		return data
	}
	data.Projects = projects
	data.Selected = selected
	if data.Selected == "" && len(projects) > 0 {
		data.Selected = projects[0].ID
	}

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

func (h *siteSettingsAdminHandle) fillProjectSettings(c *gin.Context, data *siteSettingsData) {
	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: data.Selected})
	if err != nil || project == nil {
		logger.Scene("settings").With("project", data.Selected).Error(err, "读取站点工程失败")
		return
	}
	data.Name = project.Name

	fields := projectcontract.ParseSiteSettings(project.Settings)
	data.SiteName = fields.SiteName
	data.SiteDesc = fields.SiteDesc
	data.ContactEmail = fields.ContactEmail
	data.GA4MeasurementID = fields.GA4MeasurementID
	data.SearchConsoleVerification = fields.SearchConsoleVerification

	data.HeadScripts = fields.HeadScripts
	data.BodyScripts = fields.BodyScripts
	data.IndexNowKey = fields.IndexNowKey
	data.NotFoundHTML = fields.NotFoundHTML

	data.ShippingBaseFeeYuan = projectdto.FormatCentsAsYuan(fields.ShippingBaseFee)
	data.ShippingFreeThresholdYuan = projectdto.FormatCentsAsYuan(fields.ShippingFreeThreshold)

	data.URLPatterns = buildURLPatternRows(fields.URLPatterns)

	data.Locales = h.localeRowsOf(c, data.Selected)

	data.LangURLMode = strings.TrimSpace(fields.LangURLMode)
	if data.LangURLMode == "" {

		data.LangURLMode = string(i18n.DefaultSiteLangURLMode())
	}
	data.LangURLOffWarning = langURLOffWarning(data.LangURLMode, data.Locales)
}

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

func (h *siteSettingsAdminHandle) SaveSiteSettings(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))

	if projectID == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrProjectRequired, "请先选择要操作的站点工程"),
			backpath.FromRequest(c.Request.URL.Query(), "/admin/settings", "/admin", "project"), siteSettingsBackMsg)
		return
	}
	back := siteSettingsBack(c)
	if name == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrSiteSettingsNameRequired, "工程与站点名称不能为空"),
			back, siteSettingsBackMsg)
		return
	}

	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {

		if err != nil {
			logger.Scene("settings").With("project", projectID).Error(err, "读取站点工程失败")
		}

		projectJump(c, false, projectMsgOf(projectenums.ErrProjectNotFound, "站点工程不存在"),
			backpath.FromRequest(c.Request.URL.Query(), "/admin/settings", "/admin", "project"), siteSettingsBackMsg)
		return
	}

	patterns := map[string]string{}
	for _, k := range siteurl.KnownKinds {
		raw := strings.TrimSpace(c.PostForm("urlPattern_" + k.Kind))
		if raw == "" {
			continue
		}
		if perr := siteurl.ValidatePattern(raw); perr != nil {

			projectJump(c, false, projectErrMsg(c, "site_settings", projectenums.ErrProjectInternal, perr),
				back, siteSettingsBackMsg)
			return
		}
		patterns[k.Kind] = pathkit.NormalizeSitePath(raw)
	}
	ga4ID := ""
	if raw := strings.TrimSpace(c.PostForm("ga4MeasurementId")); raw != "" {
		id, ok := builder.NormalizeGA4MeasurementID(raw)
		if !ok {
			projectJump(c, false, projectMsgOf(projectenums.ErrGA4IDInvalid, "GA4 测量 ID 格式不合法（形如 G-XXXXXXXXXX，只允许字母与数字）"),
				back, siteSettingsBackMsg)
			return
		}
		ga4ID = id
	}

	searchConsoleToken := ""
	if raw := strings.TrimSpace(c.PostForm("searchConsoleVerification")); raw != "" {
		token, ok := builder.NormalizeSearchConsoleVerification(raw)
		if !ok {
			projectJump(c, false, projectMsgOf(projectenums.ErrGSCVerificationInvalid, "Search Console 验证 token 格式不合法（base64url：字母、数字、- 与 _，8~128 位）"),
				back, siteSettingsBackMsg)
			return
		}
		searchConsoleToken = token
	}

	rawHead := strings.TrimSpace(c.PostForm("headScripts"))
	rawBody := strings.TrimSpace(c.PostForm("bodyScripts"))
	headScripts, headOK := builder.NormalizeHeadScripts(rawHead)
	bodyScripts, bodyOK := builder.NormalizeBodyScripts(rawBody)
	if (rawHead != "" && !headOK) || (rawBody != "" && !bodyOK) {
		data := h.buildSiteSettingsData(c, projectID)
		data.HeadScripts = rawHead
		data.BodyScripts = rawBody
		if rawHead != "" && !headOK {
			data.HeadScriptsError = projectenums.SiteSettingsHeadScriptsInvalid
		}
		if rawBody != "" && !bodyOK {
			data.BodyScriptsError = projectenums.SiteSettingsBodyScriptsInvalid
		}
		pageview.Render(c, data)
		return
	}

	baseCents, baseErr := projectdto.ParseYuanToCents(c.PostForm("shippingBaseFee"))
	if baseErr != nil {
		projectJump(c, false, projectMsgOf(projectenums.ErrShippingBaseFeeInvalid, "基础运费金额不合法（负数 / 非数字 / 超上限）"),
			back, siteSettingsBackMsg)
		return
	}
	thresholdCents, thresholdErr := projectdto.ParseYuanToCents(c.PostForm("shippingFreeThreshold"))
	if thresholdErr != nil {
		projectJump(c, false, projectMsgOf(projectenums.ErrShippingFreeThresholdInvalid, "满额免运费门槛金额不合法（同上）"),
			back, siteSettingsBackMsg)
		return
	}

	if _, nerr := projectdto.NormalizeShippingPolicy(baseCents, thresholdCents); nerr != nil {
		projectJump(c, false, projectMsgOf(projectenums.ErrShippingFreeThresholdInvalid, "满额免运费门槛金额不合法（同上）"),
			back, siteSettingsBackMsg)
		return
	}

	notFoundHTML := strings.TrimSpace(c.PostForm("notFoundHtml"))
	if len(notFoundHTML) > maxNotFoundHTMLLen {
		projectJump(c, false, projectMsgOf(projectenums.ErrNotFoundHTMLTooLong, "自定义 404 页内容过长（上限 32 KiB）"),
			back, siteSettingsBackMsg)
		return
	}
	settingsJSON, err := mergeSiteSettings(project.Settings, projectcontract.SiteSettings{
		SiteName:         strings.TrimSpace(c.PostForm("siteName")),
		SiteDesc:         strings.TrimSpace(c.PostForm("siteDesc")),
		ContactEmail:     strings.TrimSpace(c.PostForm("contactEmail")),
		GA4MeasurementID: ga4ID,

		SearchConsoleVerification: searchConsoleToken,
		IndexNowKey:               strings.TrimSpace(c.PostForm("indexNowKey")),

		HeadScripts:  headScripts,
		BodyScripts:  bodyScripts,
		NotFoundHTML: notFoundHTML,
		URLPatterns:  patterns,

		ShippingBaseFee:       baseCents,
		ShippingFreeThreshold: thresholdCents,
	})
	if err != nil {
		projectJump(c, false, projectErrMsg(c, "settings", projectenums.ErrProjectInternal, err),
			back, siteSettingsBackMsg)
		return
	}

	settingsChanged := siteSettingsDiffer(project.Settings, settingsJSON)
	if _, err := h.projects.Update(c.Request.Context(), &projectcontract.UpdateReq{
		ID: projectID, Name: name, Settings: settingsJSON,
	}); err != nil {
		projectJump(c, false, projectErrMsg(c, "settings", projectenums.ErrProjectInternal, err),
			back, siteSettingsBackMsg)
		return
	}

	h.markPagesStaleForSiteSettingsChange(c.Request.Context(), projectID, settingsChanged)
	projectJump(c, true, siteSettingsSavedMsg, back, siteSettingsBackMsg)
}

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

func (h *siteSettingsAdminHandle) markPagesStaleForSiteSettingsChange(ctx context.Context, projectID string, changed bool) {
	if !changed || h.pages == nil {
		return
	}
	if err := h.pages.MarkStaleForI18n(ctx); err != nil {
		logger.Scene("settings").With("project", projectID).Error(err, "站点设置变更后标记全站待重建失败")
	}
}

const maxNotFoundHTMLLen = 32 * 1024

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

	setInt64 := func(key string, value int64) error {
		if value == 0 {
			delete(obj, key)
			return nil
		}
		obj[key] = json.RawMessage(strconv.FormatInt(value, 10))
		return nil
	}

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

type localeRow struct {
	Lang      string
	IsDefault bool
	Enabled   bool
}

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
	pageview.Render(c, &projectdto.LocaleRowsView{Locales: rows})
}

func (h *siteSettingsAdminHandle) SaveSiteLocales(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if projectID == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrProjectRequired, "请先选择要操作的站点工程"),
			backpath.FromRequest(c.Request.URL.Query(), "/admin/settings", "/admin", "project"), siteSettingsBackMsg)
		return
	}
	back := siteSettingsBack(c)

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

	modeRaw := strings.TrimSpace(c.PostForm("langURLMode"))
	modeChanged := false
	if modeRaw != "" {
		mode, merr := i18n.ParseSiteLangURLMode(modeRaw)
		if merr != nil {

			projectJump(c, false, projectMsgOf(projectenums.ErrLangURLModeInvalid, "语言 URL 方案取值非法（可选 off / default_plain / all_prefix）"),
				back, siteSettingsBackMsg)
			return
		}

		current, cerr := h.projects.SiteLangURLMode(c.Request.Context(), projectID)
		if cerr != nil {

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
			projectJump(c, false, projectErrMsg(c, "settings", projectenums.ErrProjectInternal, err),
				back, siteSettingsBackMsg)
			return
		}
	}
	after, err := h.projects.SaveLocales(c.Request.Context(), req)
	if err != nil {

		logger.Scene("settings").With("project", projectID).Error(err, "保存语言清单失败")
		data := h.buildSiteSettingsData(c, projectID)
		data.Locales = rows
		data.LocaleError = siteSettingsLocalesInval
		pageview.Render(c, data)
		return
	}
	h.markPagesStaleForLocaleChange(c.Request.Context(), projectID, before, after)
	if modeChanged {

		if h.pages != nil {
			if serr := h.pages.MarkStaleForI18n(c.Request.Context()); serr != nil {
				logger.Scene("settings").With("project", projectID).Error(serr, "语言 URL 方案变更后标记全站待重建失败")
			}
		}
	}
	projectJump(c, true, localesSavedMsg, back, siteSettingsBackMsg)
}

func (h *siteSettingsAdminHandle) saveLangURLMode(ctx context.Context, projectID string, mode i18n.SiteLangURLMode) error {
	project, err := h.projects.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		if err == nil {
			err = fmt.Errorf("站点工程不存在: %s", projectID)
		}
		return err
	}

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

const (
	themePageMsgTitle = "MsgThemesTitle"
)

type themeAdminHandle struct {
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
	blocks   blockcontract.BlockService
}

type themeRow struct {
	ID        string
	ProjectID string
	Name      string
	IsActive  bool
	CreatedAt string
	UpdatedAt string
}

type themeManageData struct {
	Title             string
	Menu              string
	Projects          []projectcontract.ProjectResp
	SelectedProjectID string
	Themes            []themeRow

	Err projectMsg

	NoProjectEmpty bool
}

func (h *themeAdminHandle) renderThemeManage(c *gin.Context, data *themeManageData) {
	data.NoProjectEmpty = len(data.Projects) == 0 && data.Err.Empty()
	pageview.Render(c, data)
}

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

		data.Err = projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err)
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

			data.Err = projectErrMsg(c, "theme", projectenums.ErrThemeInternal, terr)
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

// bindThemeCreateForm 绑定新建主题表单；失败已由这里按域错误出口写回。
func bindThemeCreateForm(c *gin.Context) (*projectdto.ThemeCreateReq, bool) {
	form, err := binding.ParseForm[projectdto.ThemeCreateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "project", err)
		return nil, false
	}
	return form, true
}

func (h *themeAdminHandle) CreateTheme(c *gin.Context) {
	form, ok := bindThemeCreateForm(c)
	if !ok {
		return
	}
	projectID, name := form.ProjectID, form.Name
	back := themesBack(c)

	if projectID == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeProjectIDEmpty, "工程 ID 不能为空"),
			back, themesBackMsg)
		return
	}
	if name == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeNameRequired, "主题名称不能为空"),
			back, themesBackMsg)
		return
	}
	theme, err := h.projects.CreateTheme(c.Request.Context(), &projectcontract.ThemeCreateReq{
		ProjectID: projectID, Name: name,
	})
	if err != nil {
		projectJump(c, false, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackMsg)
		return
	}

	if err := h.pages.AttachThemeToUnassigned(c.Request.Context(), projectID, theme.ID); err != nil {
		logger.Scene("page").With("project", projectID).With("theme", theme.ID).Warn("回填未挂主题页面失败")
	}
	projectJump(c, true, themeCreatedMsg, back, themesBackMsg)
}

// bindThemeForm 绑定主题写表单；失败已由这里按域错误出口写回。
func bindThemeForm(c *gin.Context) (*projectdto.ThemeActivateReq, bool) {
	req, err := binding.ParseForm[projectdto.ThemeActivateReq](c)
	if err != nil || req.ID == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeIDRequired, "缺少主题 id"),
			themesBack(c), themesBackMsg)
		return nil, false
	}
	return req, true
}

func (h *themeAdminHandle) ActivateTheme(c *gin.Context) {
	form, ok := bindThemeForm(c)
	if !ok {
		return
	}
	id := form.ID
	back := themesBack(c)
	if err := h.projects.ActivateTheme(c.Request.Context(), &projectcontract.ThemeActivateReq{ID: id}); err != nil {
		projectJump(c, false, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackMsg)
		return
	}

	theme, err := h.projects.GetTheme(c.Request.Context(), id)
	if err != nil || theme == nil {
		logger.Scene("theme").With("theme_id", id).Error(err, "激活后取主题失败，整站换皮未执行")
		projectJump(c, false, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackMsg)
		return
	}
	themeJSON, structureJSON, err := themeSnapshots(theme.Settings)
	if err != nil {
		logger.Scene("theme").With("theme_id", theme.ID).Error(err, "序列化主题快照失败")
		projectJump(c, false, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackMsg)
		return
	}
	if err := h.pages.ReskinProjectForTheme(c.Request.Context(), theme.ProjectID, theme.ID, themeJSON, structureJSON); err != nil {
		projectJump(c, false, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackMsg)
		return
	}
	projectJump(c, true, themeActivatedMsg, back, themesBackMsg)
}

func (h *themeAdminHandle) refreshThemePages(c *gin.Context, themeID string, settingsJSON json.RawMessage) int {
	ctx := c.Request.Context()
	themeJSON, structureJSON, err := themeSnapshots(settingsJSON)
	if err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "序列化主题快照失败")
		return http.StatusInternalServerError
	}

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

	if len(pageIDs) > 0 {
		batch := append([]string(nil), pageIDs...)
		go func() {

			if err := h.pages.RebuildStale(context.Background(), batch); err != nil {
				logger.Scene("theme").With("theme_id", themeID).With("count", len(batch)).
					Error(err, "主题变更后的自动重建失败（页面保持待重建，等下次触发）")
			}
		}()
	}
	return 0
}

func themeSnapshots(settings json.RawMessage) (themeJSON, structureJSON json.RawMessage, err error) {

	ts, err := builder.ParseThemeSettings(settings)
	if err != nil {
		return nil, nil, err
	}
	themeJSON, err = json.Marshal(ts)
	if err != nil {
		return nil, nil, err
	}

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

	if len(s.Slots) > 0 {
		fields["slots"] = s.Slots
	}
	structureJSON, err = json.Marshal(fields)
	if err != nil {
		return nil, nil, err
	}
	return themeJSON, structureJSON, nil
}

func (h *themeAdminHandle) DeleteTheme(c *gin.Context) {
	form, ok := bindThemeForm(c)
	if !ok {
		return
	}
	id := form.ID
	back := themesBack(c)
	projectID := ""
	if theme, err := h.projects.GetTheme(c.Request.Context(), id); err == nil && theme != nil {
		projectID = theme.ProjectID
	}
	if err := h.projects.DeleteTheme(c.Request.Context(), id); err != nil {
		projectJump(c, false, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err),
			back, themesBackMsg)
		return
	}
	if projectID != "" {
		if active, err := h.projects.GetActiveTheme(c.Request.Context(), projectID); err == nil && active != nil {
			if err := h.pages.AttachThemeToUnassigned(c.Request.Context(), projectID, active.ID); err != nil {
				logger.Scene("page").With("project", projectID).With("theme", active.ID).Warn("删除主题后转挂页面失败")
			}
		}
	}
	projectJump(c, true, themeDeletedMsg, back, themesBackMsg)
}

func (h *themeAdminHandle) ThemeRedirect(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/admin/themes")
}

type panelOption struct {
	Value    string
	Label    string
	Selected bool
}

type panelOptionView struct {
	Value         string
	LabelKey      string
	LabelFallback string
	Selected      bool
}

type themeField struct {
	Label   string
	Path    string
	Name    string
	Kind    string
	Options []panelOption
}

type themeFieldGroup struct {
	Title  string
	Fields []themeField
}

type themeFieldView struct {
	LabelKey      string
	LabelFallback string

	Path    string
	Name    string
	Kind    string
	Value   string
	Options []panelOptionView

	Invalid       bool
	ErrorKey      string
	ErrorFallback string
}

type themeFieldGroupView struct {
	TitleKey      string
	TitleFallback string
	Fields        []themeFieldView
}

var themeLabelKeys = map[string]string{

	"颜色":      projectenums.ThemeSettingsLabelColors,
	"排版 · 标题": projectenums.ThemeSettingsLabelTypographyHeading,
	"排版 · 正文": projectenums.ThemeSettingsLabelTypographyBody,
	"排版 · 链接": projectenums.ThemeSettingsLabelTypographyLink,
	"按钮":      projectenums.ThemeSettingsLabelButton,
	"表面":      projectenums.ThemeSettingsLabelSurface,
	"图片":      projectenums.ThemeSettingsLabelImages,
	"动效":      projectenums.ThemeSettingsLabelMotion,

	"主色":     projectenums.ThemeSettingsLabelPrimaryColor,
	"次色":     projectenums.ThemeSettingsLabelSecondaryColor,
	"点缀色":    projectenums.ThemeSettingsLabelAccentColor,
	"成功色":    projectenums.ThemeSettingsLabelSuccessColor,
	"警告色":    projectenums.ThemeSettingsLabelWarningColor,
	"危险色":    projectenums.ThemeSettingsLabelDangerColor,
	"正文色":    projectenums.ThemeSettingsLabelBodyColor,
	"标题色":    projectenums.ThemeSettingsLabelHeadingColor,
	"页面背景":   projectenums.ThemeSettingsLabelPageBackground,
	"卡片底色":   projectenums.ThemeSettingsLabelCardSurface,
	"边框色":    projectenums.ThemeSettingsLabelBorderColor,
	"字重":     projectenums.ThemeSettingsLabelFontWeight,
	"基准字号":   projectenums.ThemeSettingsLabelHeadingSize,
	"标题下间距":  projectenums.ThemeSettingsLabelHeadingSpacing,
	"字体":     projectenums.ThemeSettingsLabelFontFamily,
	"字号":     projectenums.ThemeSettingsLabelBodySize,
	"行高":     projectenums.ThemeSettingsLabelLineHeight,
	"悬停色":    projectenums.ThemeSettingsLabelLinkHoverColor,
	"下划线":    projectenums.ThemeSettingsLabelUnderline,
	"背景":     projectenums.ThemeSettingsLabelButtonBackground,
	"文字色":    projectenums.ThemeSettingsLabelButtonTextColor,
	"圆角":     projectenums.ThemeSettingsLabelRadius,
	"纵向内边距":  projectenums.ThemeSettingsLabelPaddingY,
	"横向内边距":  projectenums.ThemeSettingsLabelPaddingX,
	"悬停背景":   projectenums.ThemeSettingsLabelHoverBackground,
	"悬停文字色":  projectenums.ThemeSettingsLabelHoverTextColor,
	"边框宽":    projectenums.ThemeSettingsLabelBorderWidth,
	"边框样式":   projectenums.ThemeSettingsLabelBorderStyle,
	"阴影":     projectenums.ThemeSettingsLabelShadow,
	"全局圆角":   projectenums.ThemeSettingsLabelGlobalRadius,
	"默认阴影":   projectenums.ThemeSettingsLabelDefaultShadow,
	"懒加载默认":  projectenums.ThemeSettingsLabelLazyDefault,
	"懒加载骨架屏": projectenums.ThemeSettingsLabelLazySkeleton,
	"过渡时长":   projectenums.ThemeSettingsLabelTransitionDuration,
	"缓动":     projectenums.ThemeSettingsLabelEasing,
	"默认入场":   projectenums.ThemeSettingsLabelEntrance,

	"默认":            projectenums.ThemeSettingsLabelOptDefault,
	"无":             projectenums.ThemeSettingsLabelOptNone,
	"小":             projectenums.ThemeSettingsLabelOptSmall,
	"中":             projectenums.ThemeSettingsLabelOptMedium,
	"大":             projectenums.ThemeSettingsLabelOptLarge,
	"特大":            projectenums.ThemeSettingsLabelOptXLarge,
	"直角":            projectenums.ThemeSettingsLabelOptRadiusNone,
	"胶囊":            projectenums.ThemeSettingsLabelOptRadiusPill,
	"实线":            projectenums.ThemeSettingsLabelOptBorderSolid,
	"虚线":            projectenums.ThemeSettingsLabelOptBorderDashed,
	"点线":            projectenums.ThemeSettingsLabelOptBorderDotted,
	"双线":            projectenums.ThemeSettingsLabelOptBorderDouble,
	"无边框":           projectenums.ThemeSettingsLabelOptBorderNone,
	"悬停时":           projectenums.ThemeSettingsLabelOptUnderlineHover,
	"始终":            projectenums.ThemeSettingsLabelOptUnderlineAlways,
	"开启":            projectenums.ThemeSettingsLabelOptOn,
	"关闭":            projectenums.ThemeSettingsLabelOptOff,
	"开启（推荐）":        projectenums.ThemeSettingsLabelOptOnRecommended,
	"常规":            projectenums.ThemeSettingsLabelOptWeightRegular,
	"中等":            projectenums.ThemeSettingsLabelOptWeightMedium,
	"半粗":            projectenums.ThemeSettingsLabelOptWeightSemiBold,
	"粗体":            projectenums.ThemeSettingsLabelOptWeightBold,
	"淡入":            projectenums.ThemeSettingsLabelOptFadeIn,
	"淡入·上":          projectenums.ThemeSettingsLabelOptFadeUp,
	"上滑":            projectenums.ThemeSettingsLabelOptSlideUp,
	"缩放":            projectenums.ThemeSettingsLabelOptZoomIn,
	"无（瞬时）":         projectenums.ThemeSettingsLabelOptDurationNone,
	"8px（推荐）":       projectenums.ThemeSettingsLabelOptRadius8Recommended,
	"10px（推荐）":      projectenums.ThemeSettingsLabelOptRadius10Recommended,
	"12px（推荐）":      projectenums.ThemeSettingsLabelOptRadius12Recommended,
	"16px（推荐）":      projectenums.ThemeSettingsLabelOptSize16Recommended,
	"20px（推荐）":      projectenums.ThemeSettingsLabelOptSize20Recommended,
	"32px（推荐）":      projectenums.ThemeSettingsLabelOptSize32Recommended,
	"200ms（推荐）":     projectenums.ThemeSettingsLabelOptDuration200Recommended,
	"1.4（紧凑）":       projectenums.ThemeSettingsLabelOptLineHeight14Tight,
	"1.6（常用）":       projectenums.ThemeSettingsLabelOptLineHeight16Common,
	"1.7（推荐）":       projectenums.ThemeSettingsLabelOptLineHeight17Recommended,
	"1.8（宽松）":       projectenums.ThemeSettingsLabelOptLineHeight18Loose,
	"系统默认（推荐）":      projectenums.ThemeSettingsLabelOptFontSystemRecommended,
	"默认（跟随主题）":      projectenums.ThemeSettingsLabelOptFontThemeDefault,
	"Inter / 现代无衬线": projectenums.ThemeSettingsLabelOptFontInter,
	"Georgia（衬线）":   projectenums.ThemeSettingsLabelOptFontGeorgia,
	"衬线体":           projectenums.ThemeSettingsLabelOptFontSerif,
	"等宽体":           projectenums.ThemeSettingsLabelOptFontMono,
	"中文无衬线":         projectenums.ThemeSettingsLabelOptFontChineseSans,
}

func themeLabelFact(label string) (key, fallback string) {
	return themeLabelKeys[label], label
}

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

		{Label: "懒加载默认", Path: "images.lazyLoad", Name: "images.lazyLoad", Kind: "select",
			Options: optionsOf("on", "开启（推荐）", "off", "关闭")},

		{Label: "懒加载骨架屏", Path: "images.skeleton", Name: "images.skeleton", Kind: "select",
			Options: optionsOf("false", "关闭", "true", "开启")},
	}},
	{Title: "动效", Fields: []themeField{
		{Label: "过渡时长", Path: "motion.transitionDuration", Name: "motion.duration", Kind: "select",
			Options: optionsOf("", "默认", "0ms", "无（瞬时）", "100ms", "100ms", "150ms", "150ms", "200ms", "200ms（推荐）", "300ms", "300ms", "500ms", "500ms")},
		{Label: "缓动", Path: "motion.easing", Name: "motion.easing", Kind: "select",
			Options: optionsOf("", "默认", "ease", "Ease", "ease-out", "Ease Out", "linear", "Linear")},
		{Label: "默认入场", Path: "motion.defaultEntrance", Name: "motion.entrance", Kind: "select",
			Options: optionsOf("", "无", "fade-in", "淡入", "fade-in-up", "淡入·上", "slide-in-up", "上滑", "zoom-in", "缩放")},
	}},
}

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

func weightOptions() []panelOption {
	return optionsOf("", "默认", "400", "常规", "500", "中等", "600", "半粗", "700", "粗体")
}

func optionsOf(pairs ...string) []panelOption {
	out := make([]panelOption, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, panelOption{Value: pairs[i], Label: pairs[i+1]})
	}
	return out
}

func buildThemeGroups(settings map[string]any) []themeFieldGroupView {
	groups := make([]themeFieldGroupView, 0, len(themeFieldGroups))
	for _, g := range themeFieldGroups {
		titleKey, titleFallback := themeLabelFact(g.Title)
		view := themeFieldGroupView{
			TitleKey: titleKey, TitleFallback: titleFallback,
			Fields: make([]themeFieldView, 0, len(g.Fields)),
		}
		for _, f := range g.Fields {
			value := propString(settings, f.Path)
			labelKey, labelFallback := themeLabelFact(f.Label)
			item := themeFieldView{
				LabelKey: labelKey, LabelFallback: labelFallback,
				Path: f.Path, Name: f.Name, Kind: f.Kind, Value: value,
			}
			for _, o := range f.Options {
				optionKey, optionFallback := themeLabelFact(o.Label)
				item.Options = append(item.Options, panelOptionView{
					Value: o.Value, LabelKey: optionKey, LabelFallback: optionFallback,
					Selected: o.Value == value,
				})
			}
			view.Fields = append(view.Fields, item)
		}
		groups = append(groups, view)
	}
	return groups
}

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

func workbenchGlobalPanel(c *gin.Context) {
	themeID := strings.TrimSpace(c.PostForm("themeId"))
	if themeID == "" {

		pageview.Render(c, &projectdto.GlobalPanelView{ThemeID: ""})
		return
	}
	var settings map[string]any
	if raw := c.PostForm("settings"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &settings)
	}

	pageview.Render(c, &projectdto.GlobalPanelView{ThemeID: themeID, Groups: buildThemeGroups(settings)})
}

const (
	themeSettingsMsgTitle = "MsgThemeSettingsTitle"
)

const themeSettingsSavedTextFallback = "主题设置已保存，该主题下页面已标记待重建 —— 重新构建后新样式才会出现在访问面。"

const themeSettingsFieldInvalidFallback = "这个值不合法：只能填颜色（#3d444f）、尺寸（16px）这类 CSS 值。"

type themeSettingsData struct {
	Title     string
	Menu      string
	ThemeID   string
	ThemeName string
	ProjectID string

	PColor     string
	TColor     string
	BgColor    string
	SColor     string
	BdColor    string
	FontFamily string

	ThemeSettingsJSON string

	Groups []themeFieldGroupView

	HeaderBlockID string
	FooterBlockID string

	HeaderBlocks []blockOption
	FooterBlocks []blockOption

	AnnouncementBlocks  []blockOption
	AnnouncementBlockID string

	HeaderTemplateID string
	FooterTemplateID string

	SlotTemplates map[string]string

	HeaderTemplateOptions []structureTemplateOptionView
	FooterTemplateOptions []structureTemplateOptionView

	Err projectMsg
}

type structureTemplateOptionView struct {
	ID string

	Name string

	SuffixKey      string
	SuffixFallback string
	Selected       bool
}

type blockOption struct {
	ID string

	NameKey      string
	NameFallback string
	Kind         string
}

type themeSettingsJSON struct {
	builder.ThemeSettings
	HeaderBlockID string `json:"headerBlockId,omitempty"`
	FooterBlockID string `json:"footerBlockId,omitempty"`

	Slots map[string]string `json:"slots,omitempty"`

	HeaderTemplateID string            `json:"headerTemplateId,omitempty"`
	FooterTemplateID string            `json:"footerTemplateId,omitempty"`
	SlotTemplates    map[string]string `json:"slotTemplates,omitempty"`
}

func (h *themeAdminHandle) ThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.Query("id"))
	if themeID == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeIDRequired, "缺少主题 id"),
			themesBack(c), themesBackMsg)
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeNotFound, "主题不存在"),
			themesBack(c), themesBackMsg)
		return
	}
	pageview.Render(c, data)
}

func (h *themeAdminHandle) loadThemeSettings(c *gin.Context, themeID string) *themeSettingsData {
	ctx := c.Request.Context()
	theme, err := h.projects.GetTheme(ctx, themeID)
	if err != nil || theme == nil {
		return nil
	}
	data := &themeSettingsData{
		Title: themeSettingsMsgTitle, Menu: "themes",
		ThemeID: theme.ID, ThemeName: theme.Name, ProjectID: theme.ProjectID,
	}

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

	var s themeSettingsJSON
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &s); err != nil {
			logger.Scene("page").With("theme_id", themeID).Error(err, "解析主题结构绑定 JSON 失败")
		}
	}
	data.HeaderBlockID = s.HeaderBlockID
	data.FooterBlockID = s.FooterBlockID

	data.HeaderTemplateID = s.HeaderTemplateID
	data.FooterTemplateID = s.FooterTemplateID
	data.SlotTemplates = s.SlotTemplates

	if opts, oerr := h.projects.StructureTemplateOptions(ctx, theme.ProjectID); oerr != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(oerr, "读取结构模板候选失败（下拉候选为空，页面其余部分照常）")
	} else {
		for _, o := range opts {

			view := structureTemplateOptionView{ID: o.ID, Name: o.Name}
			if o.IsDefault {
				view.SuffixKey = projectenums.ThemeSettingsStructureCurrent
				view.SuffixFallback = "（当前生效）"
			}
			if o.EntityType == "footer" {
				view.Selected = o.ID == data.FooterTemplateID
				data.FooterTemplateOptions = append(data.FooterTemplateOptions, view)
				continue
			}
			view.Selected = o.ID == data.HeaderTemplateID
			data.HeaderTemplateOptions = append(data.HeaderTemplateOptions, view)
		}
	}

	data.AnnouncementBlockID = s.Slots["announcement"]

	var rawSettings map[string]any
	if len(theme.Settings) > 0 {
		_ = json.Unmarshal(theme.Settings, &rawSettings)
	}
	data.Groups = buildThemeGroups(rawSettings)

	if blocks, err := h.blocks.List(ctx, &blockcontract.ListReq{ProjectID: theme.ProjectID}); err == nil {

		unset := blockOption{
			NameKey:      projectenums.ThemeSettingsBlockUnset,
			NameFallback: "（未设置）",
		}
		data.HeaderBlocks = []blockOption{unset}
		data.FooterBlocks = []blockOption{unset}
		data.AnnouncementBlocks = []blockOption{unset}
		for _, b := range blocks {
			opt := blockOption{ID: b.ID, NameFallback: b.Name, Kind: b.Kind}
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

func (h *themeAdminHandle) SaveThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.PostForm("id"))

	if themeID == "" {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeIDRequired, "缺少主题 id"),
			themesBack(c), themesBackMsg)
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		projectJump(c, false, projectMsgOf(projectenums.ErrThemeNotFound, "主题不存在"),
			themesBack(c), themesBackMsg)
		return
	}

	back := themeSettingsBack(c)

	ts := &builder.ThemeSettings{

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

	settingsJSON, err := json.Marshal(themeSettingsJSON{
		ThemeSettings: *ts,
		HeaderBlockID: strings.TrimSpace(c.PostForm("headerBlockId")),
		FooterBlockID: strings.TrimSpace(c.PostForm("footerBlockId")),
		Slots:         themeSlotFormValues(c),

		HeaderTemplateID: structureTemplateFormValue(c, "headerTemplateId", data.HeaderTemplateID),
		FooterTemplateID: structureTemplateFormValue(c, "footerTemplateId", data.FooterTemplateID),
		SlotTemplates:    themeSlotTemplateFormValues(c, data.SlotTemplates),
	})
	if err != nil {

		themeSettingsFailPage(c, data, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err), nil, nil)
		return
	}

	if _, verr := builder.ParseThemeSettings(settingsJSON); verr != nil {

		logger.Scene("theme").With("theme_id", themeID).Error(verr, "主题设置校验失败")
		themeSettingsFailPage(c, data, projectMsgOf(projectenums.MsgThemeSettingsInvalid, "主题设置不合法"), verr, settingsJSON)
		return
	}
	if _, err := h.projects.UpdateTheme(c.Request.Context(), &projectcontract.ThemeUpdateReq{
		ID: themeID, Name: data.ThemeName, Settings: settingsJSON,
	}); err != nil {
		themeSettingsFailPage(c, data, projectErrMsg(c, "theme", projectenums.ErrThemeInternal, err), nil, settingsJSON)
		return
	}

	if code := h.refreshThemePages(c, themeID, settingsJSON); code != 0 {

		projectJump(c, false, projectMsgOf(projectenums.MsgThemeSettingsRefreshFailed,
			"主题设置已保存，但整站页面刷新失败 —— 稍后在页面列表里重建即可，不必重填。"),
			back, themeSettingsBackMsg)
		return
	}

	projectJump(c, true, themeSettingsSavedMsg, back, themeSettingsBackMsg)
}

func themeSettingsFailPage(c *gin.Context, data *themeSettingsData, msg projectMsg, fieldErr error, echoSettings json.RawMessage) {

	data.Err = msg
	if len(echoSettings) > 0 {
		data.ThemeSettingsJSON = string(echoSettings)
	}
	themeSettingsEchoForm(c, data)
	if name, _, _ := themeSettingsInvalidField(data.Groups, fieldErr); name != "" {
		themeSettingsMarkInvalid(data.Groups, name,
			projectMsg{Key: projectenums.ThemeSettingsErrFieldInvalid, Fallback: themeSettingsFieldInvalidFallback})
	}
	pageview.Render(c, data)
}

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

func themeSettingsFormValue(form url.Values, key string) (string, bool) {
	values, ok := form[key]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

func themeSettingsMarkTemplateSelected(options []structureTemplateOptionView, current string) {
	for i := range options {
		options[i].Selected = options[i].ID == current
	}
}

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

			if f.Value == bad || strings.TrimSpace(f.Value) == bad {
				return f.Name, f.LabelFallback, bad
			}
		}
	}
	return "", "", bad
}

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

func themeSettingsMarkInvalid(groups []themeFieldGroupView, name string, msg projectMsg) {
	if name == "" {
		return
	}
	for gi := range groups {
		for fi := range groups[gi].Fields {
			if groups[gi].Fields[fi].Name == name {
				groups[gi].Fields[fi].Invalid = true
				groups[gi].Fields[fi].ErrorKey = msg.Key
				groups[gi].Fields[fi].ErrorFallback = msg.Fallback
				return
			}
		}
	}
}

func structureTemplateFormValue(c *gin.Context, key, current string) string {
	if strings.TrimSpace(c.PostForm("structureTemplateFields")) != "1" {
		return firstNonEmpty(strings.TrimSpace(c.PostForm(key)), current)
	}
	return strings.TrimSpace(c.PostForm(key))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

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
