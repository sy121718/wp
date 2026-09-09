package dashboardhttp

// 站点设置页：展示/编辑站点基础信息（站点名/简介/联系邮箱）。
// 数据源为 project 模块 SiteSettings（projects.settings JSON），经 project 契约读写。
// 其余站点级能力（SEO/多语言/统计等）尚未落地，页面以「待实现能力」占位标注。

import (
	"encoding/json"
	"net/http"
	"strings"

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
		"Locales":      d.Locales,
		"LocaleError":  d.LocaleError,
		"LocaleSaved":  d.LocaleSaved,
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
	fields := parseSiteSettingsFields(project.Settings)
	data.SiteName = fields.SiteName
	data.SiteDesc = fields.SiteDesc
	data.ContactEmail = fields.ContactEmail
	// 语言清单（project_locales）：站点「有哪几种语言」的唯一真源，与构建/路由同源。
	data.Locales = h.localeRowsOf(c, data.Selected)
}

// SaveSiteSettings 保存站点设置（POST /admin/settings/save）。
// 仅更新基础信息字段并合入现有 SiteSettings JSON（其余保留），再经 project.Update 持久化。
func (h *Handle) SaveSiteSettings(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	if projectID == "" || name == "" {
		c.String(http.StatusBadRequest, "工程与站点名称不能为空")
		return
	}
	// 读取当前 settings，合并基础字段。
	project, err := h.projects.Detail(c.Request.Context(), &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		c.String(http.StatusNotFound, "站点工程不存在")
		return
	}
	cur := parseSiteSettingsFields(project.Settings)
	cur.SiteName = strings.TrimSpace(c.PostForm("siteName"))
	cur.SiteDesc = strings.TrimSpace(c.PostForm("siteDesc"))
	cur.ContactEmail = strings.TrimSpace(c.PostForm("contactEmail"))
	settingsJSON, err := json.Marshal(cur)
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

// siteSettingsFields 站点设置基础字段（settings JSON 对象，缺失字段返回空串）。
// 后续扩展站点级能力时在此追加字段，其余 settings 键保持不变。
type siteSettingsFields struct {
	SiteName     string `json:"siteName,omitempty"`
	SiteDesc     string `json:"siteDesc,omitempty"`
	ContactEmail string `json:"contactEmail,omitempty"`
}

// parseSiteSettingsFields 解析并返回基础站点信息字段。
func parseSiteSettingsFields(raw json.RawMessage) *siteSettingsFields {
	f := &siteSettingsFields{}
	if len(raw) == 0 {
		return f
	}
	_ = json.Unmarshal(raw, f)
	return f
}
