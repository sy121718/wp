package projecthttp

// site_settings_admin_pages.go — 站点设置页与站点语言清单管理
// （自 dashboard/inbound/http/site_settings_handle.go 与 site_locales_handle.go 迁入）。
//
// 页面：/admin/settings（GET + POST 保存）、/admin/settings/locales/rows（HTMX 行片段）、
// /admin/settings/locales/save（全量保存）。数据经 project 契约读写 projects.settings；
// 语言清单变更后的「全站标记待重建」经 page 契约编排（依赖方向 page → project，
// project 反向持有 page 契约由装配层注入）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/siteurl"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 站点设置页文案（i18n key，与 dashboard enums 迁移前同值）。
const (
	siteSettingsMsgTitle = "MsgSiteSettingsTitle"
	// siteSettingsLocalesInval 语言清单校验失败的就地提示（回渲染 + 回显用户输入，
	// 不经 ?err=，所以不在 projectPageErrKeys 里）。
	siteSettingsLocalesInval = "MsgSiteLocalesInvalid"
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
	// NotFoundHTML 站点自定义 404 页内容（发布时写到激活目录根的 404.html；
	// 空 = 不配置，且会删除既有 404.html，SEO-013）。
	NotFoundHTML string
	// URLPatterns URL 规则编辑行（各实体类型的详情页路径模式，WP 固定链接的等价物）。
	URLPatterns []urlPatternRow

	// Locales 站点语言清单编辑行（多语言 P3，project_locales）。
	Locales []localeRow
	// LocaleError 语言清单校验失败提示（空=无错误）。
	LocaleError string
	// LocaleSaved 语言清单刚保存成功（?locales_saved=1，PRG 回跳提示）。
	LocaleSaved bool
	// IndexNowKey IndexNow 协议密钥（SEO-022）。
	IndexNowKey string
	// LangURLOffWarning 启用多语言但 url_mode=off 时的提示（I18N-016）。
	LangURLOffWarning bool
	// LangURLMode 语言 URL 方案（多语言开关）当前生效值：工程 settings 覆盖值优先，
	// 未配置时回显进程启动值 —— 面板展示的必须「就是现在跑着的那个」，
	// 否则作者看着 off 以为关了、实际进程在跑 default_plain。
	LangURLMode string
	// Err 上一次保存失败的提示（?err= 经读侧白名单，空 = 无提示）。
	//
	// 与 LocaleError 的分工：LocaleError 是**语言清单**校验失败的就地提示（回渲染 + 回显用户输入，
	// 见 SaveSiteLocales）；Err 是本页其余保存路径（站点信息 / GA4 / GSC / 404 页 / 语言 URL 方案）
	// 经 303 回带的一句提示。两者可以同时为空 —— 那时页面上没有任何提示条。
	Err string
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
		"NotFoundHTML":              d.NotFoundHTML,
		"URLPatterns":               d.URLPatterns,
		"Locales":                   d.Locales,
		"LocaleError":               d.LocaleError,
		"LocaleSaved":               d.LocaleSaved,
		"IndexNowKey":               d.IndexNowKey,
		"LangURLOffWarning":         d.LangURLOffWarning,
		"LangURLMode":               d.LangURLMode,
		"Err":                       d.Err,
	}
}

// siteSettingsBackURL 站点设置页的回跳 URL（保留当前工程，见 projectErrRedirect）。
//
// projectID 来自表单，必须转义后再拼：它直接进 Location 头，
// 未转义时含 & / # 的值能把后面的 err= 参数截断（最坏是提示静默消失，看不出原因）。
func siteSettingsBackURL(projectID string) string {
	if strings.TrimSpace(projectID) == "" {
		return "/admin/settings"
	}
	return "/admin/settings?project=" + url.QueryEscape(strings.TrimSpace(projectID))
}

// siteSettingsAdminHandle 站点设置页处理器。
type siteSettingsAdminHandle struct {
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
}

// NewSiteSettingsAdminHandle 测试与外部装配用的导出构造（窄两个契约，均可 nil 降级）。
func NewSiteSettingsAdminHandle(projects projectcontract.ProjectService, pages pagecontract.PageService) *siteSettingsAdminHandle {
	return &siteSettingsAdminHandle{projects: projects, pages: pages}
}

// SiteSettings 站点设置页（GET /admin/settings）。
func (h *siteSettingsAdminHandle) SiteSettings(c *gin.Context) {
	data := h.buildSiteSettingsData(c, strings.TrimSpace(c.Query("project")))
	data.LocaleSaved = strings.TrimSpace(c.Query("locales_saved")) == "1"
	// 上一次保存失败的提示（读侧白名单：手拼的 ?err= 一律落空串）。
	// **装载失败优先**：它是这次请求真实发生的事，URL 里那条是上一次写失败的旧提示。
	if data.Err == "" {
		data.Err = projectPageErrText(c, c.Query("err"))
	}
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
	data.IndexNowKey = fields.IndexNowKey
	data.NotFoundHTML = fields.NotFoundHTML
	// URL 规则：当前配置（可能为空）+ 默认模式（placeholder，"留空 = 用默认"要看得见）。
	data.URLPatterns = buildURLPatternRows(fields.URLPatterns)
	// 语言清单（project_locales）：站点「有哪几种语言」的唯一真源，与构建/路由同源。
	data.Locales = h.localeRowsOf(c, data.Selected)
	// 语言 URL 方案（多语言开关）：工程覆盖值优先；未配置时回显进程当前值 ——
	// 面板必须展示「现在真的在跑的那个」，否则开关的语义就是假的。
	data.LangURLMode = strings.TrimSpace(fields.LangURLMode)
	if data.LangURLMode == "" {
		data.LangURLMode = string(i18n.SiteLangURLModeValue())
	}
	data.LangURLOffWarning = langURLOffWarning(data.Locales)
}

// langURLOffWarning 启用多种语言且 url_mode=off 时提示（I18N-016）。
func langURLOffWarning(rows []localeRow) bool {
	if i18n.SiteLangURLModeValue() != i18n.SiteLangURLModeOff {
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
func (h *siteSettingsAdminHandle) SaveSiteSettings(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	// 缺参拆两条见 CreateTheme 的同类注释：工程缺失时连「回哪一页」都定不下来，
	// 只能回设置页入口；名称缺失时工程是知道的，回该工程的设置页。
	if projectID == "" {
		projectErrRedirect(c, siteSettingsBackURL(""), response.TranslateMessage(c, projectenums.ErrProjectRequired))
		return
	}
	backURL := siteSettingsBackURL(projectID)
	if name == "" {
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrSiteSettingsNameRequired))
		return
	}
	// 读取当前 settings，合并本页字段。
	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		// 工程不存在：原文只进日志（可能是别的实例刚删了它），对外一句受控文案。
		if err != nil {
			logger.Scene("settings").With("project", projectID).Error(err, "读取站点工程失败")
		}
		projectErrRedirect(c, siteSettingsBackURL(""), response.TranslateMessage(c, projectenums.ErrProjectNotFound))
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
			shell.PageErrorBadRequest(c, "site_settings", perr)
			return
		}
		patterns[k.Kind] = siteurl.Normalize(raw)
	}
	ga4ID := ""
	if raw := strings.TrimSpace(c.PostForm("ga4MeasurementId")); raw != "" {
		id, ok := builder.NormalizeGA4MeasurementID(raw)
		if !ok {
			projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrGA4IDInvalid))
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
			projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrGSCVerificationInvalid))
			return
		}
		searchConsoleToken = token
	}
	// 自定义 404 页（SEO-013）：只卡长度 —— 存的是一份完整 HTML 文档，没有可校验的
	// 「正确形状」（不同于 GA4 ID / GSC token）。超长会连同 projects.settings 整列一起
	// 被每次读设置解析，所以在入口拒绝，而不是存进去等发布时才发现。
	notFoundHTML := strings.TrimSpace(c.PostForm("notFoundHtml"))
	if len(notFoundHTML) > maxNotFoundHTMLLen {
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrNotFoundHTMLTooLong))
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
		NotFoundHTML:              notFoundHTML,
		URLPatterns:               patterns,
	})
	if err != nil {
		projectErrRedirect(c, backURL, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err))
		return
	}
	if _, err := h.projects.Update(c.Request.Context(), &projectcontract.UpdateReq{
		ID: projectID, Name: name, Settings: settingsJSON,
	}); err != nil {
		projectErrRedirect(c, backURL, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err))
		return
	}
	c.Redirect(http.StatusSeeOther, backURL)
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
//   - 全量保存 POST /admin/settings/locales/save：普通表单 POST + 303 回跳（PRG），
//     与同页「保存设置」一致。
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
		projectErrRedirect(c, siteSettingsBackURL(""), response.TranslateMessage(c, projectenums.ErrProjectRequired))
		return
	}
	localesBackURL := siteSettingsBackURL(projectID)
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
			// 注释原先承诺「校验失败时回渲染设置页并给出提示」，代码却是一行纯文本 ——
			// 本批把出口补齐（303 回设置页 + ?err=，合法取值就写在文案里）。
			projectErrRedirect(c, localesBackURL, response.TranslateMessage(c, projectenums.ErrLangURLModeInvalid))
			return
		}
		if mode != i18n.SiteLangURLModeValue() {
			modeChanged = true
		}
		if err := h.saveLangURLMode(c.Request.Context(), projectID, mode); err != nil {
			projectErrRedirect(c, localesBackURL, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err))
			return
		}
	}
	after, err := h.projects.SaveLocales(c.Request.Context(), req)
	if err != nil {
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
	c.Redirect(http.StatusSeeOther, "/admin/settings?project="+projectID+"&locales_saved=1")
}

// saveLangURLMode 把语言 URL 方案持久化到工程 settings 并热更新进程值。
//
// 两步缺一不可：只热更新不落库，重启后被 config.yaml 打回；只落库不热更新，
// 作者保存后看到的站点行为不变（产物要等重建才发现）。i18n 的 setter 自带锁，
// 并发请求下后写者胜 —— 与「设置页最后一次保存生效」的直觉一致。
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
	i18n.SetSiteLangURLMode(mode)
	return nil
}

// restoreLangURLMode 启动恢复：把设置页持久化的语言 URL 方案（projects.settings.langURLMode）
// 恢复成进程当前值。
//
// 为什么必须在启动时做：langURLMode 是对进程配置（config.yaml i18n.site_lang_url_mode）的
// **站点级覆盖**，运行时唯一读取口是 pkg/i18n 的包级变量 —— 设置页保存时只热更新了内存，
// 不在启动时读回，重启后就被 config.yaml 打回旧值，设置页的开关就只剩半个生命周期。
//
// 取值口径与设置页一致：取**第一个**配置了该键的工程（工程选择器默认选中第一个工程，
// 两处同源）；恢复失败只记日志不阻断启动 —— 进程配置仍是兜底真源，失败的表现是
// 「开关退回 config.yaml 的值」，而不是站点起不来。
func restoreLangURLMode(svc projectcontract.ProjectService) {
	if svc == nil {
		return
	}
	projects, err := svc.List(context.Background())
	if err != nil {
		logger.Scene("settings").Error(err, "恢复语言 URL 方案失败：读取工程列表失败")
		return
	}
	for _, p := range projects {
		fields := projectcontract.ParseSiteSettings(p.Settings)
		raw := strings.TrimSpace(fields.LangURLMode)
		if raw == "" {
			continue
		}
		mode, perr := i18n.ParseSiteLangURLMode(raw)
		if perr != nil {
			logger.Scene("settings").With("project", p.ID).With("raw", raw).
				Error(perr, "恢复语言 URL 方案失败：取值非法，保留进程配置")
			continue
		}
		i18n.SetSiteLangURLMode(mode)
		logger.Scene("settings").With("project", p.ID).With("mode", string(mode)).
			Info("已从站点设置恢复语言 URL 方案")
		return
	}
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
