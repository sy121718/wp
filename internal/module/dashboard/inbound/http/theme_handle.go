package dashboardhttp

// 主题管理（多主题体系，020_themes.sql）：主题 = 站点前端项目
// （全局颜色/字体/页眉页脚引用），一次一套激活，页面挂接主题。
// 切换激活主题 = 整站前端换皮，页面内容不动。
//
// 交互遵循后台规范：普通表单 POST + 303 重定向整页刷新。

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectdto "go_wp/internal/module/project/dto"

	"github.com/gin-gonic/gin"
)

// ---- 主题管理列表页（GET /admin/themes?project=X） ----

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
	Projects          []projectdto.ProjectResp
	SelectedProjectID string
	Themes            []themeRow
}

// templateMap 转 Jet 模板键 map（layout 以小写 title/menu 取值）。
func (d *themeManageData) templateMap() gin.H {
	return gin.H{
		"title":            d.Title,
		"menu":             d.Menu,
		"Projects":         d.Projects,
		"SelectedProject":  d.SelectedProjectID,
		"Themes":           d.Themes,
	}
}

// ThemeManage 主题管理页：列出工程全部主题，支持新建/激活/删除。
func (h *Handle) ThemeManage(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data := &themeManageData{
		Title:    dashboardenums.MsgThemesTitle,
		Menu:     "themes",
		Projects: projects,
	}
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProjectID = sel
	} else if len(projects) > 0 {
		data.SelectedProjectID = projects[0].ID
	}
	if data.SelectedProjectID != "" {
		themes, err := h.projects.ListThemes(c.Request.Context(), data.SelectedProjectID)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		data.Themes = make([]themeRow, 0, len(themes))
		for _, t := range themes {
			data.Themes = append(data.Themes, themeRow{
				ID: t.ID, ProjectID: t.ProjectID, Name: t.Name, IsActive: t.IsActive,
				CreatedAt: t.CreatedAt.Format("2006-01-02 15:04"),
				UpdatedAt: t.UpdatedAt.Format("2006-01-02 15:04"),
			})
		}
	}
	c.HTML(http.StatusOK, "admin/theme", data.templateMap())
}

// CreateTheme 新建主题（POST /admin/themes/create）。
// 工程首个主题自动激活；创建后把工程内未挂主题的历史页面回填到新主题。
func (h *Handle) CreateTheme(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	if projectID == "" || name == "" {
		c.String(http.StatusBadRequest, "工程与主题名称不能为空")
		return
	}
	theme, err := h.projects.CreateTheme(c.Request.Context(), &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: name,
	})
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	// 回填：该工程 theme_id 为空的历史页面挂到新主题（失败不阻塞，可再次保存触发）。
	if err := h.pages.AttachThemeToUnassigned(c.Request.Context(), projectID, theme.ID); err != nil {
		log.Printf("[theme] 回填未挂主题页面失败 project=%s theme=%s: %v", projectID, theme.ID, err)
	}
	c.Redirect(http.StatusSeeOther, "/admin/themes?project="+projectID)
}

// ActivateTheme 激活主题（POST /admin/themes/activate）。
// 页面内容不动：页面挂接各自主题，编译取页面文档内 settings.theme 快照。
func (h *Handle) ActivateTheme(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少主题 id")
		return
	}
	if err := h.projects.ActivateTheme(c.Request.Context(), &projectdto.ThemeActivateReq{ID: id}); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, h.backToThemes(c))
}

// DeleteTheme 删除主题（POST /admin/themes/delete；激活态由 service 拒绝）。
// 删除后该主题页面被 FK 置空（ON DELETE SET NULL），统一转挂到工程当前
// 激活主题，避免页面长期脱挂；工程无主题时保持 NULL（由下次建主题回填）。
func (h *Handle) DeleteTheme(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少主题 id")
		return
	}
	projectID := ""
	if theme, err := h.projects.GetTheme(c.Request.Context(), id); err == nil && theme != nil {
		projectID = theme.ProjectID
	}
	if err := h.projects.DeleteTheme(c.Request.Context(), id); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	if projectID != "" {
		if active, err := h.projects.GetActiveTheme(c.Request.Context(), projectID); err == nil && active != nil {
			if err := h.pages.AttachThemeToUnassigned(c.Request.Context(), projectID, active.ID); err != nil {
				log.Printf("[theme] 删除主题后转挂页面失败 project=%s theme=%s: %v", projectID, active.ID, err)
			}
		}
	}
	c.Redirect(http.StatusSeeOther, h.backToThemes(c))
}

// backToThemes 从表单隐藏域恢复来源工程，保持列表页聚焦不变。
func (h *Handle) backToThemes(c *gin.Context) string {
	if projectID := strings.TrimSpace(c.PostForm("projectId")); projectID != "" {
		return "/admin/themes?project=" + projectID
	}
	return "/admin/themes"
}

// ---- 单主题设置页（GET /admin/themes/settings?id=X） ----

// themeSettingsData 单主题设置页数据（全局颜色/字体/页眉页脚引用）。
type themeSettingsData struct {
	Title      string
	Menu       string
	ThemeID    string
	ThemeName  string
	ProjectID  string
	PColor     string
	TColor     string
	BgColor    string
	SColor     string
	BdColor    string
	FontFamily string
	// HeaderPageID/FooterPageID 全局页眉/页脚引用的页面 id（编译装配待接入，当前仅存储）。
	HeaderPageID string
	FooterPageID string
	// Pages 该主题下页面列表，作为页眉/页脚引用候选。
	Pages []pageOption
}

// pageOption 页眉/页脚引用候选下拉项。
type pageOption struct {
	ID   string
	Path string
	Kind string
}

// templateMap 转 Jet 模板键 map。
func (d *themeSettingsData) templateMap() gin.H {
	return gin.H{
		"title":        d.Title,
		"menu":         d.Menu,
		"ThemeID":      d.ThemeID,
		"ThemeName":    d.ThemeName,
		"ProjectID":    d.ProjectID,
		"PColor":       d.PColor,
		"TColor":       d.TColor,
		"BgColor":      d.BgColor,
		"SColor":       d.SColor,
		"BdColor":      d.BdColor,
		"FontFamily":   d.FontFamily,
		"HeaderPageID": d.HeaderPageID,
		"FooterPageID": d.FooterPageID,
		"Pages":        d.Pages,
	}
}

// themeSettingsJSON 与 020_themes.sql 注释约定的 settings 结构对齐。
type themeSettingsJSON struct {
	Colors       map[string]string `json:"colors,omitempty"`
	FontFamily   string            `json:"fontFamily,omitempty"`
	HeaderPageID string            `json:"headerPageId,omitempty"`
	FooterPageID string            `json:"footerPageId,omitempty"`
}

// ThemeSettings 单主题设置页。
func (h *Handle) ThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.Query("id"))
	if themeID == "" {
		c.String(http.StatusBadRequest, "缺少主题 id")
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		c.String(http.StatusNotFound, "主题不存在")
		return
	}
	c.HTML(http.StatusOK, "admin/theme_settings", data.templateMap())
}

// loadThemeSettings 组装单主题设置页数据；主题不存在返回 nil。
func (h *Handle) loadThemeSettings(c *gin.Context, themeID string) *themeSettingsData {
	ctx := c.Request.Context()
	theme, err := h.projects.GetTheme(ctx, themeID)
	if err != nil || theme == nil {
		return nil
	}
	data := &themeSettingsData{
		Title: dashboardenums.MsgThemeSettingsTitle, Menu: "themes",
		ThemeID: theme.ID, ThemeName: theme.Name, ProjectID: theme.ProjectID,
	}
	var s themeSettingsJSON
	if len(theme.Settings) > 0 {
		_ = json.Unmarshal(theme.Settings, &s)
	}
	if s.Colors != nil {
		data.PColor = s.Colors["primary"]
		data.TColor = s.Colors["text"]
		data.BgColor = s.Colors["background"]
		data.SColor = s.Colors["surface"]
		data.BdColor = s.Colors["border"]
	}
	data.FontFamily = s.FontFamily
	data.HeaderPageID = s.HeaderPageID
	data.FooterPageID = s.FooterPageID
	// 页眉/页脚引用候选：挂在该主题下的页面。
	if pages, err := h.pages.List(ctx, theme.ID); err == nil {
		data.Pages = make([]pageOption, 0, len(pages))
		for _, p := range pages {
			data.Pages = append(data.Pages, pageOption{ID: p.ID, Path: p.DraftPath, Kind: p.Kind})
		}
	}
	return data
}

// SaveThemeSettings 保存单主题设置（POST /admin/themes/settings/save）：
// 写回 themes.settings；颜色/字体批量合入该主题下全部页面文档（settings.theme 快照）。
// 页眉页脚引用是编译装配元数据，仅存储不合入页面文档。
func (h *Handle) SaveThemeSettings(c *gin.Context) {
	themeID := strings.TrimSpace(c.PostForm("id"))
	if themeID == "" {
		c.String(http.StatusBadRequest, "缺少主题 id")
		return
	}
	data := h.loadThemeSettings(c, themeID)
	if data == nil {
		c.String(http.StatusNotFound, "主题不存在")
		return
	}
	colors := map[string]string{}
	for _, key := range []string{"primary", "text", "background", "surface", "border"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			colors[key] = v
		}
	}
	settings := themeSettingsJSON{
		Colors:       colors,
		FontFamily:   strings.TrimSpace(c.PostForm("fontFamily")),
		HeaderPageID: strings.TrimSpace(c.PostForm("headerPageId")),
		FooterPageID: strings.TrimSpace(c.PostForm("footerPageId")),
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := h.projects.UpdateTheme(c.Request.Context(), &projectdto.ThemeUpdateReq{
		ID: themeID, Name: data.ThemeName, Settings: settingsJSON,
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// 颜色/字体快照合入该主题下全部页面（编译端 ThemeSettings 只认这两个键）。
	snapshot, _ := json.Marshal(map[string]any{"colors": colors, "fontFamily": settings.FontFamily})
	if err := h.pages.RefreshThemeForTheme(c.Request.Context(), themeID, snapshot); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/themes/settings?id="+themeID)
}

// ThemeRedirect 旧入口 /admin/theme 301 到新主题管理页。
func (h *Handle) ThemeRedirect(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/admin/themes")
}
