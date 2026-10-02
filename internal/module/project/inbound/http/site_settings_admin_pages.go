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
	"reflect"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
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
	// HeadScripts 站点自定义 Head 代码（构建期注入 </head> 之前；空 = 不注入，PIPE-8）。
	HeadScripts string
	// BodyScripts 站点自定义 Body 代码（构建期注入 </body> 之前；空 = 不注入，PIPE-8）。
	BodyScripts string
	// HeadScriptsError / BodyScriptsError 自定义代码校验失败的就地提示（空 = 无错误）。
	//
	// 与 Err 的分工：Err 是 ?err= 通道（303 回带的一句文案，读侧白名单过滤）；
	// 这两个是**就地回渲染**的提示 —— 与 LocaleError 同一路，因为这两个字段的内容
	// 是几百字节的脚本，303 一跳表单就空了，用户填的代码跟着丢（?err= 只带一句文案、
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
	// 自定义注入代码（PIPE-8）：形状判据的唯一出口是 builder.NormalizeHeadScripts /
	// NormalizeBodyScripts（保存与注入共用同一份判据，否则会出现「后台存进去了、
	// 产物里却没有」这种最难排查的分歧）；非法（含结构性标签 / 超 16 KiB）时**不落库**。
	//
	// 出口用就地回渲染而不是 projectErrRedirect(303 + ?err=)：这两个字段装的是几百字节
	// 的脚本，303 一跳用户看到的是空表单 —— 他刚贴进去的代码就丢了，而 ?err= 只带一句
	// 文案、带不动表单内容。回渲染把输入原样带回，他只需改那一处（与语言清单保存失败
	// 同一条路，见下方的 SaveSiteLocales）。
	//
	// 附带一层：这两条提示**不进 ?err= 通道**，所以它们的 i18n key 不参与
	// projectPageErrKeys 的「必须在迁移里登记」那条硬约束（本批不新增迁移）。
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
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrShippingBaseFeeInvalid))
		return
	}
	thresholdCents, thresholdErr := projectdto.ParseYuanToCents(c.PostForm("shippingFreeThreshold"))
	if thresholdErr != nil {
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrShippingFreeThresholdInvalid))
		return
	}
	// 组合判据（与读取侧同一个函数）：目前只有「非负 + 上限」，已由上面两次解析把住；
	// 这一步留着是为了让「什么算合法规则」只有一处定义 —— 将来加「门槛与基础运费的关系」
	// 这类跨字段规则时，不会漏掉保存侧这条链路。失败时提示落在门槛字段上：跨字段规则
	// 必然是「门槛与另一个字段的关系」，指到门槛最可定位。
	if _, nerr := projectdto.NormalizeShippingPolicy(baseCents, thresholdCents); nerr != nil {
		projectErrRedirect(c, backURL, response.TranslateMessage(c, projectenums.ErrShippingFreeThresholdInvalid))
		return
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
		projectErrRedirect(c, backURL, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err))
		return
	}
	// 构建可见的字段**是否真的变了**：changed 决定保存后要不要标 stale。
	// 比较的是同一个 mergeSiteSettings 产出的规范化 JSON（键顺序稳定），
	// 因此「只点了一次保存、什么都没改」不会误判成变化 —— 那会把保存按钮变成一次全量重建。
	settingsChanged := siteSettingsDiffer(project.Settings, settingsJSON)
	if _, err := h.projects.Update(c.Request.Context(), &projectcontract.UpdateReq{
		ID: projectID, Name: name, Settings: settingsJSON,
	}); err != nil {
		projectErrRedirect(c, backURL, projectErrParam(c, "settings", projectenums.ErrProjectInternal, err))
		return
	}
	// 保存成功之后才标记（失败路径不标：配置没落库，标了只会白重建一次）。
	h.markPagesStaleForSiteSettingsChange(c.Request.Context(), projectID, settingsChanged)
	c.Redirect(http.StatusSeeOther, backURL)
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
