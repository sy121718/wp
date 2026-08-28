package dashboardhttp

// 页面管理列表页（后台「页面」入口）：列出/新建站点工程与页面，
// 行内直达可视化工作台。交互遵循后台 HTMX 规范：HTMX 请求返回
// Jet 片段，否则完整页面/重定向。

import (
	"encoding/json"
	"net/http"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"

	"github.com/gin-gonic/gin"
)

// pagesPageData 页面列表页数据。
// 模板键统一小写（admin/layout.html 以 {{.title}}/{{.menu}} 取值，
// Jet 对 map 键不做大小写兜底）。
type pagesPageData struct {
	Title    string
	Menu     string
	Projects []projectdto.ProjectResp
	Pages    []pageRow
}

// templateMap 转为模板所需的小写键 map。
func (d *pagesPageData) templateMap() gin.H {
	return gin.H{
		"title":    d.Title,
		"menu":     d.Menu,
		"Projects": d.Projects,
		"Pages":    d.Pages,
	}
}

// pageRow 列表行投影（含状态文案）。
type pageRow struct {
	ID        string
	ProjectID string
	Kind      string
	DraftPath string
	Active    bool
	Staged    bool
	Stale     bool
	Version   int64
	UpdatedAt string
}

// PagesList 页面列表页。
func (h *Handle) PagesList(c *gin.Context) {
	data, err := h.buildPagesData(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.HTML(http.StatusOK, "admin/pages", data.templateMap())
}

// buildPagesData 组装列表页数据。
func (h *Handle) buildPagesData(c *gin.Context) (*pagesPageData, error) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		return nil, err
	}
	pages, err := h.pages.List(c.Request.Context())
	if err != nil {
		return nil, err
	}
	rows := make([]pageRow, 0, len(pages))
	for _, p := range pages {
		rows = append(rows, pageRow{
			ID: p.ID, ProjectID: p.ProjectID, Kind: p.Kind,
			DraftPath: p.DraftPath, Active: p.ActiveArtifactID != nil,
			Staged: p.StagedArtifactID != nil, Stale: p.Stale,
			Version: p.DraftVersion, UpdatedAt: p.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	return &pagesPageData{
		Title: dashboardenums.MsgPagesTitle, Menu: "pages",
		Projects: projects, Pages: rows,
	}, nil
}

// CreateProject 新建站点工程（HTMX 表单提交，成功后整页刷新列表）。
func (h *Handle) CreateProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.String(http.StatusBadRequest, "项目名称不能为空")
		return
	}
	if _, err := h.projects.Create(c.Request.Context(), &projectdto.CreateReq{
		Name: name, Settings: json.RawMessage("{}"),
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/pages")
}

// CreatePage 新建页面（默认空白草稿，创建后可进工作台编辑）。
func (h *Handle) CreatePage(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	path := strings.TrimSpace(c.PostForm("draftPath"))
	if projectID == "" || path == "" {
		c.String(http.StatusBadRequest, "项目与页面路径不能为空")
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 默认空白草稿：layout.mode 为编译端必填校验项（full/boxed）。
	if _, err := h.pages.Create(c.Request.Context(), &pagedto.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         path,
		DraftDocument:     json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/pages")
}
