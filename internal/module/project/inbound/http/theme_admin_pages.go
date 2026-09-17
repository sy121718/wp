package projecthttp

// theme_admin_pages.go — 主题管理页（自 dashboard/inbound/http/theme_handle.go 迁入）。
//
// 页面：/admin/themes（列表/新建/激活/删除）、/admin/theme（旧入口 301）。
// 主题换皮编排（AttachThemeToUnassigned / ReskinProjectForTheme / Refresh*）经 page
// 契约完成：语言清单归 project，「全站页面刷新」能力归 page，依赖方向 page → project，
// project 反向持有 page 契约是装配层注入的编排点（brief 明确保留）。

import (
	"encoding/json"
	"net/http"
	"strings"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 主题域页面文案（i18n key，与 dashboard enums 迁移前同值）。
const (
	themePageMsgInternal = "MsgInternalError"
	themePageMsgTitle    = "MsgThemesTitle"
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
// NewThemeAdminHandle 测试与外部装配用的导出构造（blocks 可空：仅主题设置页的候选块读取用）。
func NewThemeAdminHandle(projects projectcontract.ProjectService, pages pagecontract.PageService,
	blocks blockcontract.BlockService) *themeAdminHandle {
	return &themeAdminHandle{projects: projects, pages: pages, blocks: blocks}
}

func (h *themeAdminHandle) ThemeManage(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, themePageMsgInternal)
		return
	}
	data := &themeManageData{
		Title:    themePageMsgTitle,
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
			response.ErrorWithMessage(c, http.StatusInternalServerError, themePageMsgInternal)
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
	c.HTML(http.StatusOK, "admin/theme", shell.Prepare(c, data.templateMap()))
}

// CreateTheme 新建主题（POST /admin/themes/create）。
// 工程首个主题自动激活；创建后把工程内未挂主题的历史页面回填到新主题。
func (h *themeAdminHandle) CreateTheme(c *gin.Context) {
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
		c.String(http.StatusInternalServerError, themePageMsgInternal)
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
func (h *themeAdminHandle) ActivateTheme(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少主题 id")
		return
	}
	if err := h.projects.ActivateTheme(c.Request.Context(), &projectcontract.ThemeActivateReq{ID: id}); err != nil {
		logger.Scene("theme").With("theme_id", id).Error(err, "激活主题失败")
		c.String(http.StatusInternalServerError, themePageMsgInternal)
		return
	}
	// 激活已提交：取新激活主题的 ProjectID 与 Settings，编排刷新整站页面。
	theme, err := h.projects.GetTheme(c.Request.Context(), id)
	if err != nil || theme == nil {
		logger.Scene("theme").With("theme_id", id).Error(err, "激活后取主题失败，整站换皮未执行")
		c.Redirect(http.StatusSeeOther, h.backToThemes(c))
		return
	}
	themeJSON, structureJSON, err := themeSnapshots(theme.Settings)
	if err != nil {
		logger.Scene("theme").With("theme_id", theme.ID).Error(err, "序列化主题快照失败")
		c.Redirect(http.StatusSeeOther, h.backToThemes(c))
		return
	}
	if err := h.pages.ReskinProjectForTheme(c.Request.Context(), theme.ProjectID, theme.ID, themeJSON, structureJSON); err != nil {
		logger.Scene("theme").With("theme_id", theme.ID).With("project", theme.ProjectID).Error(err, "整站换皮失败")
		c.String(http.StatusInternalServerError, themePageMsgInternal)
		return
	}
	c.Redirect(http.StatusSeeOther, h.backToThemes(c))
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
		c.String(http.StatusInternalServerError, themePageMsgInternal)
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
func (h *themeAdminHandle) backToThemes(c *gin.Context) string {
	if projectID := strings.TrimSpace(c.PostForm("projectId")); projectID != "" {
		return "/admin/themes?project=" + projectID
	}
	return "/admin/themes"
}

// ThemeRedirect 旧入口 /admin/theme 301 到新主题管理页。
func (h *themeAdminHandle) ThemeRedirect(c *gin.Context) {
	c.Redirect(http.StatusMovedPermanently, "/admin/themes")
}
