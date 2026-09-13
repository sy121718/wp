package dashboardhttp

// 站点设置页：展示/编辑站点基础信息（站点名/简介/联系邮箱）与统计代码（GA4 测量 ID）。
// 数据源为 project 模块 SiteSettings（projects.settings JSON），经 project 契约读写。
// 其余站点级能力（SEO 全局设置等）尚未落地，页面以「待实现能力」占位标注。

import (
	"encoding/json"
	"net/http"
	"strings"

	"go_wp/internal/builder"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

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

	// Locales 站点语言清单编辑行（多语言 P3，project_locales）。
	Locales []localeRow
	// LocaleError 语言清单校验失败提示（空=无错误）。
	LocaleError string
	// LocaleSaved 语言清单刚保存成功（?locales_saved=1，PRG 回跳提示）。
	LocaleSaved bool
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
		"Locales":          d.Locales,
		"LocaleError":      d.LocaleError,
		"LocaleSaved":      d.LocaleSaved,
	}
}

// SiteSettings 站点设置页（GET /admin/settings）。
func (h *Handle) SiteSettings(c *gin.Context) {
	data := h.buildSiteSettingsData(c, strings.TrimSpace(c.Query("project")))
	data.LocaleSaved = strings.TrimSpace(c.Query("locales_saved")) == "1"
	c.HTML(http.StatusOK, "admin/settings", withCSRF(c, data.templateMap()))
}

// buildSiteSettingsData 组装站点设置页数据（工程列表 + 选中工程的基础信息与语言清单）。
// selected 为空时取第一个工程；工程不存在时只渲染工程选择器。
func (h *Handle) buildSiteSettingsData(c *gin.Context, selected string) *siteSettingsData {
	data := &siteSettingsData{Title: dashboardenums.MsgSiteSettingsTitle, Menu: "settings"}
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
func (h *Handle) fillProjectSettings(c *gin.Context, data *siteSettingsData) {
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
	// 语言清单（project_locales）：站点「有哪几种语言」的唯一真源，与构建/路由同源。
	data.Locales = h.localeRowsOf(c, data.Selected)
}

// SaveSiteSettings 保存站点设置（POST /admin/settings/save）。
//
// 两条口径：
//  1. **只动本页管的键**：在现有 settings 的键集合上合并（其它模块写进同一列的键原样保留），
//     整份覆盖会把别人写的配置悄悄删掉 —— 那种丢失在页面上看不出来，只在功能失效时才暴露。
//  2. **GA4 测量 ID 在保存时就校验**（与构建期注入同一判据）：不合法直接拒绝，
//     而不是存进去等构建期静默丢弃 —— 后者运维会以为统计代码已经装好了。
func (h *Handle) SaveSiteSettings(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	if projectID == "" || name == "" {
		c.String(http.StatusBadRequest, "工程与站点名称不能为空")
		return
	}
	// 读取当前 settings，合并本页字段。
	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		c.String(http.StatusNotFound, "站点工程不存在")
		return
	}
	ga4ID := ""
	if raw := strings.TrimSpace(c.PostForm("ga4MeasurementId")); raw != "" {
		id, ok := builder.NormalizeGA4MeasurementID(raw)
		if !ok {
			c.String(http.StatusBadRequest, "GA4 测量 ID 格式不合法（形如 G-XXXXXXXXXX，只允许字母与数字）")
			return
		}
		ga4ID = id
	}
	settingsJSON, err := mergeSiteSettings(project.Settings, projectcontract.SiteSettings{
		SiteName:         strings.TrimSpace(c.PostForm("siteName")),
		SiteDesc:         strings.TrimSpace(c.PostForm("siteDesc")),
		ContactEmail:     strings.TrimSpace(c.PostForm("contactEmail")),
		GA4MeasurementID: ga4ID,
	})
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	if _, err := h.projects.Update(c.Request.Context(), &projectcontract.UpdateReq{
		ID: projectID, Name: name, Settings: settingsJSON,
	}); err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/settings?project="+projectID)
}

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
	if err := setString("contactEmail", fields.ContactEmail); err != nil {
		return nil, err
	}
	if err := setString("ga4MeasurementId", fields.GA4MeasurementID); err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}
