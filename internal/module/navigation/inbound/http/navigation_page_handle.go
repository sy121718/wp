package navigationhttp

// navigation_page_handle.go — 前台导航菜单管理页（/admin/navigations）。
// 自 dashboard 迁回本模块。与后台权限菜单（admin/menus）严格隔离：本页管理的是
// 公开站点导航（navigations 表），构建期由 core.nav 组件绑定位置后编译进静态产物。
// 交互遵循后台规范：GET 渲染整页，POST 写操作后 303 回列表页，不写新 JS。

import (
	"fmt"
	"net/http"
	"strings"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/internal/middleware/builtin"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// 页面文案（i18n key，与原 dashboard 枚举同值）。
const (
	navigationsPageTitle = "MsgNavigationsTitle"
	navFieldRequiredMsg  = "MsgFieldRequired"
)

// 导航位置（与 navigation 模块 kind 白名单对齐）。
const (
	navKindHeader = "header"
	navKindFooter = "footer"
)

// navigationPageHandle 导航菜单管理页处理器。
type navigationPageHandle struct {
	navigations navigationcontract.NavigationService
	projects    projectcontract.ProjectService
}

// NewNavigationPageHandle 创建导航菜单管理页处理器。
func NewNavigationPageHandle(navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService) *navigationPageHandle {
	return &navigationPageHandle{navigations: navigations, projects: projects}
}

// navMenuRow 菜单结构行：展平树 + 层级缩进 + 同级首末标记（控制上移/下移按钮禁用态）。
type navMenuRow struct {
	ID     string
	Title  string
	Path   string
	Depth  int
	Target string
	// TargetLabel 打开方式中文标签（模板直出，避免模板里做条件判断）。
	TargetLabel string
	// Indent 层级缩进（内联样式值，二级项缩进一格）。
	Indent     string
	SourceType string
	First      bool
	Last       bool
}

// navigationsPageData 导航菜单管理页数据。
type navigationsPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Kind            string
	Rows            []navMenuRow
	// ParentOptions 可作为父级的项（两级上限：仅顶级项可选）。
	ParentOptions []navMenuRow
	// SourceGroups 可加入菜单的来源候选（页面/文章/产品/分类，按来源分组）。
	SourceGroups []navigationcontract.SourceGroup
}

// templateMap 转为模板所需的小写键 map（layout.html 以 {{.title}}/{{.menu}} 取值）。
func (d *navigationsPageData) templateMap() gin.H {
	return gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Kind":            d.Kind,
		"Rows":            d.Rows,
		"ParentOptions":   d.ParentOptions,
		"SourceGroups":    d.SourceGroups,
	}
}

// NavigationsPage 导航菜单管理页。
func (h *navigationPageHandle) NavigationsPage(c *gin.Context) {
	data, err := h.buildNavigationsData(c)
	if err != nil {
		logger.Scene("page").Error(err, "加载导航菜单页失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	c.HTML(http.StatusOK, "admin/navigations", shell.Prepare(c, data.templateMap()))
}

// buildNavigationsData 组装页面数据（工程/位置筛选 + 菜单树展平）。
func (h *navigationPageHandle) buildNavigationsData(c *gin.Context) (*navigationsPageData, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	kind := normalizeNavKind(c.Query("kind"))
	rows := make([]navMenuRow, 0, 8)
	var groups []navigationcontract.SourceGroup
	if selected != "" {
		nodes, terr := h.navigations.Tree(ctx, selected, kind)
		if terr != nil {
			return nil, terr
		}
		flattenNavRows(nodes, 0, &rows)
		// 来源候选：页面/文章/产品/分类（依赖模块不可用时对应分组为空）。
		if groups, terr = h.navigations.SourceGroups(ctx, selected); terr != nil {
			logger.Scene("page").With("project", selected).Warn("导航来源候选加载失败，管理页仅显示自定义链接")
		}
	}
	parents := make([]navMenuRow, 0, len(rows))
	for _, r := range rows {
		if r.Depth == 0 {
			parents = append(parents, r)
		}
	}
	return &navigationsPageData{
		Title: navigationsPageTitle, Menu: "navigations",
		Projects: projects, SelectedProject: selected, Kind: kind,
		Rows: rows, ParentOptions: parents, SourceGroups: groups,
	}, nil
}

// flattenNavRows 深度优先展平菜单树（同级首末标记用于按钮禁用态）。
func flattenNavRows(nodes []*navigationdto.NavigationNode, depth int, out *[]navMenuRow) {
	for i, n := range nodes {
		label := "当前窗口"
		if n.Target == "blank" {
			label = "新标签页"
		}
		*out = append(*out, navMenuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Depth: depth,
			Target: n.Target, TargetLabel: label,
			Indent:     fmt.Sprintf("%dpx", depth*24),
			SourceType: n.SourceType,
			First:      i == 0, Last: i == len(nodes)-1,
		})
		flattenNavRows(n.Children, depth+1, out)
	}
}

// normalizeNavKind 位置参数规范化（非法值回落 header）。
func normalizeNavKind(kind string) string {
	if strings.TrimSpace(kind) == navKindFooter {
		return navKindFooter
	}
	return navKindHeader
}

// navListURL 列表页回跳地址（保留工程与位置筛选）。
func navListURL(projectID, kind string) string {
	return "/admin/navigations?project=" + projectID + "&kind=" + kind
}

// NavigationCreate 新增菜单项（默认自定义链接；指定父级则成为二级项）。
func (h *navigationPageHandle) NavigationCreate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	title := strings.TrimSpace(c.PostForm("title"))
	path := strings.TrimSpace(c.PostForm("path"))
	target := strings.TrimSpace(c.PostForm("target"))
	parentID := strings.TrimSpace(c.PostForm("parentId"))
	if projectID == "" || title == "" || path == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, navFieldRequiredMsg)
		return
	}
	req := &navigationdto.CreateReq{ProjectID: projectID, Title: title, Path: path, Kind: kind, Target: target}
	if parentID != "" {
		req.ParentID = &parentID
	}
	if _, err := h.navigations.Create(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("path", path).Error(err, "新增导航项失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// NavigationAddSource 按来源实体批量加入菜单（管理页「从已有内容添加」）。
// 标题与链接取自来源候选（页面 SEO 标题/内容标题 + 公开路径），并写入 source_type/source_id，
// 之后来源实体改标题/路径，重新构建即同步。无公开路径的候选会被跳过。
func (h *navigationPageHandle) NavigationAddSource(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	sourceType := strings.TrimSpace(c.PostForm("sourceType"))
	ids := c.PostFormArray("sourceIds")
	if projectID == "" || sourceType == "" || len(ids) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, navFieldRequiredMsg)
		return
	}
	groups, err := h.navigations.SourceGroups(ctx, projectID)
	if err != nil {
		logger.Scene("page").With("project", projectID).Error(err, "加载导航来源候选失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	candidates := make(map[string]navigationcontract.SourceCandidate, len(ids))
	for _, g := range groups {
		if g.Type != sourceType {
			continue
		}
		for _, it := range g.Items {
			candidates[it.ID] = it
		}
	}
	added, skipped := 0, 0
	for _, id := range ids {
		item, ok := candidates[id]
		if !ok || item.URL == "" {
			skipped++
			continue
		}
		sourceID := item.ID
		title := item.Title
		if title == "" {
			title = item.Label
		}
		if _, cerr := h.navigations.Create(ctx, &navigationdto.CreateReq{
			ProjectID: projectID, Title: title, Path: item.URL, Kind: kind,
			SourceType: sourceType, SourceID: &sourceID,
		}); cerr != nil {
			logger.Scene("page").With("sourceType", sourceType).With("sourceId", id).Error(cerr, "来源实体加入菜单失败")
			skipped++
			continue
		}
		added++
	}
	logger.Scene("page").With("added", added).With("skipped", skipped).Info("来源实体加入导航菜单")
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// NavigationUpdate 更新菜单项（标题/链接/打开方式，空字段保持不变）。
func (h *navigationPageHandle) NavigationUpdate(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	if id == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, navFieldRequiredMsg)
		return
	}
	req := &navigationdto.UpdateReq{ID: id}
	if v := strings.TrimSpace(c.PostForm("title")); v != "" {
		req.Title = &v
	}
	if v := strings.TrimSpace(c.PostForm("path")); v != "" {
		req.Path = &v
	}
	if v := strings.TrimSpace(c.PostForm("target")); v != "" {
		req.Target = &v
	}
	if _, err := h.navigations.Update(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("id", id).Error(err, "更新导航项失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// NavigationDelete 删除菜单项（连同其子项，避免留下孤儿节点）。
func (h *navigationPageHandle) NavigationDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	if id == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, navFieldRequiredMsg)
		return
	}
	if err := h.navigations.Delete(c.Request.Context(), &navigationdto.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("id", id).Error(err, "删除导航项失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// NavigationMove 同级上移/下移（与相邻项交换 sort_order）。
func (h *navigationPageHandle) NavigationMove(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.PostForm("id"))
	dir := strings.TrimSpace(c.PostForm("dir"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	if id == "" || (dir != "up" && dir != "down") {
		response.ErrorWithMessage(c, http.StatusBadRequest, navFieldRequiredMsg)
		return
	}
	item, err := h.navigations.Get(ctx, &navigationdto.GetReq{ID: id})
	if err != nil {
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	nodes, err := h.navigations.Tree(ctx, item.ProjectID, item.Kind)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	siblings := navSiblings(nodes, item.ParentID)
	idx := -1
	for i, s := range siblings {
		if s.ID == id {
			idx = i
			break
		}
	}
	next := idx - 1
	if dir == "down" {
		next = idx + 1
	}
	if idx < 0 || next < 0 || next >= len(siblings) {
		c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
		return
	}
	a, b := siblings[idx], siblings[next]
	if _, err = h.navigations.Update(ctx, &navigationdto.UpdateReq{ID: a.ID, SortOrder: &b.SortOrder}); err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	if _, err = h.navigations.Update(ctx, &navigationdto.UpdateReq{ID: b.ID, SortOrder: &a.SortOrder}); err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// navSiblings 返回指定父级下的同级节点列表（父级为空即顶级）。
func navSiblings(nodes []*navigationdto.NavigationNode, parentID *string) []*navigationdto.NavigationNode {
	if parentID == nil || *parentID == "" {
		return nodes
	}
	for _, n := range nodes {
		if n.ID == *parentID {
			return n.Children
		}
		if found := navSiblings(n.Children, parentID); found != nil {
			return found
		}
	}
	return nil
}

// SetupNavigationPages 注册导航菜单管理页与导航译文工作台（/admin 组，
// 中间件链由装配层统一挂好）。函数名沿用 SetupXxxPages 先例：本包已有 REST
// 路由的 SetupNavigationRoutes，不能同名。
// pageSvc 是页面契约：译文保存后按 i18n:content 依赖标记手工页面待重建
// （消费者侧断言 navTranslationPageMarker 端口，实现方不支持时降级为不标记）。
// adminPages 为 nil 时整体跳过。
func SetupNavigationPages(adminPages *gin.RouterGroup,
	navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService, pageSvc pagecontract.PageService) {
	if adminPages == nil {
		return
	}
	h := NewNavigationPageHandle(navigations, projects)
	adminPages.GET("/navigations", h.NavigationsPage)
	adminPages.POST("/navigations/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.NavigationCreate)
	adminPages.POST("/navigations/add-source", builtin.CasbinMiddlewareForPath("/api/navigation/create"), h.NavigationAddSource)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationUpdate)
	adminPages.POST("/navigations/delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), h.NavigationDelete)
	adminPages.POST("/navigations/move", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationMove)

	// 导航译文工作台（审计 I18N-007）：菜单标签不在页面文档里，页面翻译工作台看不到它，
	// 构建期靠 navigation.label 语境回填 —— 本页是那个语境的唯一维护入口。
	// 保存复用「修改导航」权限点：译文是导航项内容的一部分。
	translations := NewNavigationTranslationHandle(navigations, projects)
	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		translations.SetContentWriter(writer)
	}
	if pageSvc != nil {
		if marker, ok := pageSvc.(navTranslationPageMarker); ok {
			translations.SetPageMarker(marker)
		}
	}
	adminPages.GET("/navigations/translations", translations.NavigationTranslations)
	adminPages.POST("/navigations/translations/save", builtin.CasbinMiddlewareForPath("/api/navigation/update"), translations.SaveNavigationTranslations)
}
