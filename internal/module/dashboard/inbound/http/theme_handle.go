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

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

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
	Projects          []projectcontract.ProjectResp
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
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
			response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
	theme, err := h.projects.CreateTheme(c.Request.Context(), &projectcontract.ThemeCreateReq{
		ProjectID: projectID, Name: name,
	})
	if err != nil {
		logger.Scene("theme").Error(err, "创建主题失败")
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
//  1. 全部页面转挂到新激活主题（ReattachProjectPagesToTheme）；
//  2. 颜色/字体快照合入 settings.theme（RefreshThemeForTheme）；
//  3. 页眉/页脚绑定合入 settings.structure（RefreshStructureForTheme）；
//  4. 全部页面标记待重建（MarkStaleForTheme），下次构建即以新主题换皮。
//
// 已发布产物静态面不变，草稿重建即新主题；刷新失败不使激活回滚（激活已提交），仅记日志。
func (h *Handle) ActivateTheme(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少主题 id")
		return
	}
	if err := h.projects.ActivateTheme(c.Request.Context(), &projectcontract.ThemeActivateReq{ID: id}); err != nil {
		logger.Scene("theme").With("theme_id", id).Error(err, "激活主题失败")
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
		logger.Scene("theme").With("theme_id", id).Error(err, "删除主题失败")
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
		"title":         d.Title,
		"menu":          d.Menu,
		"ThemeID":       d.ThemeID,
		"ThemeName":     d.ThemeName,
		"ProjectID":     d.ProjectID,
		"PColor":        d.PColor,
		"TColor":        d.TColor,
		"BgColor":       d.BgColor,
		"SColor":        d.SColor,
		"BdColor":       d.BdColor,
		"FontFamily":    d.FontFamily,
		"ThemeSettings": d.ThemeSettingsJSON,
		"HeaderBlock":   d.HeaderBlockID,
		"FooterBlock":   d.FooterBlockID,
		"HeaderBlocks":  d.HeaderBlocks,
		"FooterBlocks":  d.FooterBlocks,
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
	// 页眉/页脚绑定候选：本工程的页眉/页脚类全局块。
	if blocks, err := h.blocks.List(ctx, &blockcontract.ListReq{ProjectID: theme.ProjectID}); err == nil {
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
	// 从 PostForm（点分键名）组装完整 ThemeSettings；空值直接透传为字段零值，
	// 序列化时经 omitempty 省略（未设置字段不输出 CSS 变量，组件回退自身默认）。
	ts := &builder.ThemeSettings{
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
	})
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	// ParseThemeSettings 校验（IsSafeCSSValue 白名单，防 CSS 注入）；非法返回 400。
	if _, err := builder.ParseThemeSettings(settingsJSON); err != nil {
		logger.Scene("theme").With("theme_id", themeID).Error(err, "主题设置校验失败")
		c.String(http.StatusBadRequest, dashboardenums.MsgThemeSettingsInvalid)
		return
	}
	if _, err := h.projects.UpdateTheme(c.Request.Context(), &projectcontract.ThemeUpdateReq{
		ID: themeID, Name: data.ThemeName, Settings: settingsJSON,
	}); err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
