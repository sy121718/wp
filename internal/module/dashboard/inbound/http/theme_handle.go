package dashboardhttp

// 主题管理（多主题体系，020_themes.sql）：主题 = 站点前端项目
// （全局颜色/字体/页眉页脚引用），一次一套激活，页面挂接主题。
// 切换激活主题 = 整站前端换皮，页面内容不动。
//
// 交互遵循后台规范：普通表单 POST + 303 重定向整页刷新。

import (
	"encoding/json"
	"net/http"
	"strings"

	blockdto "go_wp/internal/module/block/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/logger"

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
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProjectID,
		"Themes":          d.Themes,
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
	c.HTML(http.StatusOK, "admin/theme", withCSRF(c, data.templateMap()))
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
		logger.Scene("page").With("project", projectID).With("theme", theme.ID).Warn("回填未挂主题页面失败")
	}
	c.Redirect(http.StatusSeeOther, "/admin/themes?project="+projectID)
}

// ActivateTheme 激活主题（POST /admin/themes/activate）。
// 激活成功后对该主题所在工程的整站页面执行「换皮」：
//   1) 全部页面转挂到新激活主题（ReattachProjectPagesToTheme）；
//   2) 颜色/字体快照合入 settings.theme（RefreshThemeForTheme）；
//   3) 页眉/页脚绑定合入 settings.structure（RefreshStructureForTheme）；
//   4) 全部页面标记待重建（MarkStaleForTheme），下次构建即以新主题换皮。
// 已发布产物静态面不变，草稿重建即新主题；刷新失败不使激活回滚（激活已提交），仅记日志。
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
	// 激活已提交：取新激活主题的 ProjectID 与 Settings，编排刷新整站页面。
	theme, err := h.projects.GetTheme(c.Request.Context(), id)
	if err != nil || theme == nil {
		logger.Scene("theme").With("theme_id", id).Error(err, "激活后取主题失败，整站换皮未执行")
		c.Redirect(http.StatusSeeOther, h.backToThemes(c))
		return
	}
	// 换皮：整站页面转挂新激活主题 + 刷新快照 + 标待重建（激活不因刷新失败而回滚）。
	h.reskinProjectPages(c, theme.ID, theme.ProjectID, theme.Settings)
	c.Redirect(http.StatusSeeOther, h.backToThemes(c))
}

// reskinProjectPages 切换激活主题后的「整站换皮」编排：
// 1) 工程内全部页面转挂到新激活主题（ReattachProjectPagesToTheme）；
// 2) 刷新 settings.theme / settings.structure 快照（新主题设置）；
// 3) 全部页面标记待重建，下次构建即以新主题换皮。
// 每一步失败都只记日志并继续（激活已提交，不做回滚），保证激活结果页正常返回。
func (h *Handle) reskinProjectPages(c *gin.Context, themeID, projectID string, settings json.RawMessage) {
	ctx := c.Request.Context()
	if err := h.pages.ReattachProjectPagesToTheme(ctx, projectID, themeID); err != nil {
		logger.Scene("theme").With("theme_id", themeID).With("project", projectID).Error(err, "转挂页面到新主题失败")
	}
	themeJSON, structureJSON, err := themeSnapshots(settings)
	if err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "序列化主题快照失败")
		return
	}
	if err := h.pages.RefreshThemeForTheme(ctx, themeID, themeJSON); err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "刷新 settings.theme 快照失败")
		return
	}
	if err := h.pages.RefreshStructureForTheme(ctx, themeID, structureJSON); err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "刷新 settings.structure 快照失败")
		return
	}
	if err := h.pages.MarkStaleForTheme(ctx, themeID); err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "标记页面待重建失败")
	}
}

// refreshThemePages 把主题设置应用到已挂该主题的页面（主题设置保存路径）：
// 刷新 settings.theme/structure 快照后标记待重建；失败返回非 0 状态码。
func (h *Handle) refreshThemePages(c *gin.Context, themeID string, settingsJSON json.RawMessage) int {
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
	if err := h.pages.MarkStaleForTheme(ctx, themeID); err != nil {
		return http.StatusInternalServerError
	}
	return 0
}

// themeSnapshots 从主题设置构造页面文档快照：
// settings.theme（colors/fontFamily，编译端 :root 变量）与
// settings.structure（headerBlockId/footerBlockId，全局块槽位）。
// settings 非法时返回可读错误。
func themeSnapshots(settings json.RawMessage) (themeJSON, structureJSON json.RawMessage, err error) {
	s := themeSettingsJSON{}
	if len(settings) > 0 {
		if err = json.Unmarshal(settings, &s); err != nil {
			return nil, nil, err
		}
	}
	colors := s.Colors
	if colors == nil {
		colors = map[string]string{}
	}
	themeJSON, err = json.Marshal(map[string]any{"colors": colors, "fontFamily": s.FontFamily})
	if err != nil {
		return nil, nil, err
	}
	structureJSON, err = json.Marshal(map[string]any{
		"headerBlockId": s.HeaderBlockID,
		"footerBlockId": s.FooterBlockID,
	})
	if err != nil {
		return nil, nil, err
	}
	return themeJSON, structureJSON, nil
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
				logger.Scene("page").With("project", projectID).With("theme", active.ID).Warn("删除主题后转挂页面失败")
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

// themeSettingsData 单主题设置页数据（全局颜色/字体/页眉页脚块绑定）。
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
	// HeaderBlockID/FooterBlockID 全局页眉/页脚块绑定（编译期内联装配）。
	HeaderBlockID string
	FooterBlockID string
	// HeaderBlocks/FooterBlocks 该工程页眉/页脚候选块列表。
	HeaderBlocks []blockOption
	FooterBlocks []blockOption
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
		"HeaderBlock":  d.HeaderBlockID,
		"FooterBlock":  d.FooterBlockID,
		"HeaderBlocks": d.HeaderBlocks,
		"FooterBlocks": d.FooterBlocks,
	}
}

// themeSettingsJSON 与主题设置结构约定对齐（020_themes.sql / 021_blocks.sql 方案 C）：
// 颜色/字体为站点设计 Token；headerBlockId/footerBlockId 为全局块槽位绑定。
type themeSettingsJSON struct {
	Colors        map[string]string `json:"colors,omitempty"`
	FontFamily    string            `json:"fontFamily,omitempty"`
	HeaderBlockID string            `json:"headerBlockId,omitempty"`
	FooterBlockID string            `json:"footerBlockId,omitempty"`
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
	c.HTML(http.StatusOK, "admin/theme_settings", withCSRF(c, data.templateMap()))
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
		if err := json.Unmarshal(theme.Settings, &s); err != nil {
			logger.Scene("page").With("theme_id", themeID).Error(err, "解析主题设置 JSON 失败")
		}
	}
	if s.Colors != nil {
		data.PColor = s.Colors["primary"]
		data.TColor = s.Colors["text"]
		data.BgColor = s.Colors["background"]
		data.SColor = s.Colors["surface"]
		data.BdColor = s.Colors["border"]
	}
	data.FontFamily = s.FontFamily
	data.HeaderBlockID = s.HeaderBlockID
	data.FooterBlockID = s.FooterBlockID
	// 页眉/页脚绑定候选：本工程的页眉/页脚类全局块。
	if blocks, err := h.blocks.List(ctx, &blockdto.ListReq{ProjectID: theme.ProjectID}); err == nil {
		data.HeaderBlocks = []blockOption{{ID: "", Name: "（未设置）"}}
		data.FooterBlocks = []blockOption{{ID: "", Name: "（未设置）"}}
		for _, b := range blocks {
			opt := blockOption{ID: b.ID, Name: b.Name, Kind: b.Kind}
			switch b.Kind {
			case "header":
				data.HeaderBlocks = append(data.HeaderBlocks, opt)
			case "footer":
				data.FooterBlocks = append(data.FooterBlocks, opt)
			}
		}
	}
	return data
}

// SaveThemeSettings 保存单主题设置（POST /admin/themes/settings/save）：
// 写回 themes.settings（颜色/字体/页眉页脚块绑定）；颜色/字体批量合入该主题下
// 全部页面文档（settings.theme 快照），结构绑定合入 settings.structure；
// 保存后该主题下页面全部标待重建（新颜色/结构与块内容需重新构建生效）。
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
		Colors:        colors,
		FontFamily:    strings.TrimSpace(c.PostForm("fontFamily")),
		HeaderBlockID: strings.TrimSpace(c.PostForm("headerBlockId")),
		FooterBlockID: strings.TrimSpace(c.PostForm("footerBlockId")),
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
	// 颜色/字体快照合入 settings.theme + 页眉/页脚绑定合入 settings.structure，
	// 再标记待重建（新颜色/结构与块内容需重新构建生效）。
	if code := h.refreshThemePages(c, themeID, settingsJSON); code != 0 {
		c.String(code, "主题设置保存成功，但页面刷新失败")
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/themes/settings?id="+themeID)
}

// ThemeRedirect 旧入口 /admin/theme 301 到新主题管理页。
func (h *Handle) ThemeRedirect(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/admin/themes")
}
