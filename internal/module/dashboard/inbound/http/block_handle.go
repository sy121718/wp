package dashboardhttp

// 全局块管理页（后台「全局块」入口）：页眉/页脚/区块的列表、新建与删除。
// 块内容编辑复用工作台（/workbench?block=ID）；块保存后的 stale 传播在本模块编排：
// block 模块不感知主题/页面，跨模块组合只能依赖双方契约（模块表隔离规范）。

import (
	"encoding/json"
	"net/http"
	"strings"

	blockdto "go_wp/internal/module/block/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectdto "go_wp/internal/module/project/dto"

	"github.com/gin-gonic/gin"
)

// blockRow 全局块列表行投影。
type blockRow struct {
	ID        string
	Name      string
	Kind      string
	KindLabel string
	UpdatedAt string
}

// blocksPageData 全局块管理页数据。
type blocksPageData struct {
	Title             string
	Menu              string
	Projects          []projectdto.ProjectResp
	SelectedProjectID string
	Headers           []blockRow
	Footers           []blockRow
	Blocks            []blockRow
}

// templateMap 转 Jet 模板键 map（layout 以小写 title/menu 取值）。
func (d *blocksPageData) templateMap() gin.H {
	return gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProjectID,
		"Headers":         d.Headers,
		"Footers":         d.Footers,
		"Blocks":          d.Blocks,
	}
}

func kindLabel(kind string) string {
	switch kind {
	case "header":
		return "页眉"
	case "footer":
		return "页脚"
	default:
		return "区块"
	}
}

func toBlockRows(blocks []blockdto.BlockResp, kind string) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind != kind {
			continue
		}
		rows = append(rows, blockRow{
			ID: b.ID, Name: b.Name, Kind: b.Kind, KindLabel: kindLabel(b.Kind),
			UpdatedAt: b.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	return rows
}

// BlocksList 全局块管理页（GET /admin/blocks?project=X）。
func (h *Handle) BlocksList(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	data := &blocksPageData{
		Title:    dashboardenums.MsgBlocksTitle,
		Menu:     "blocks",
		Projects: projects,
	}
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProjectID = sel
	} else if len(projects) > 0 {
		data.SelectedProjectID = projects[0].ID
	}
	if data.SelectedProjectID != "" {
		blocks, err := h.blocks.List(c.Request.Context(), &blockdto.ListReq{ProjectID: data.SelectedProjectID})
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		data.Headers = toBlockRows(blocks, "header")
		data.Footers = toBlockRows(blocks, "footer")
		data.Blocks = toBlockRows(blocks, "block")
	}
	c.HTML(http.StatusOK, "admin/blocks", withCSRF(c, data.templateMap()))
}

// CreateBlock 新建全局块（POST /admin/blocks/create），成功后进工作台编辑内容。
func (h *Handle) CreateBlock(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	kind := strings.TrimSpace(c.PostForm("kind"))
	if projectID == "" || name == "" {
		c.String(http.StatusBadRequest, "工程与块名称不能为空")
		return
	}
	block, err := h.blocks.Create(c.Request.Context(), &blockdto.CreateReq{
		ProjectID: projectID, Name: name, Kind: kind,
	})
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/workbench?block="+block.ID)
}

// DeleteBlock 删除全局块（POST /admin/blocks/delete）。
// 删除后编排 stale：绑定该块的主题下全部页面标待重建（产物退化为无页眉/页脚）。
func (h *Handle) DeleteBlock(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少块 id")
		return
	}
	if err := h.blocks.Delete(c.Request.Context(), &blockdto.DeleteReq{ID: id}); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	h.markStaleForBlock(c, id)
	if projectID != "" {
		c.Redirect(http.StatusSeeOther, "/admin/blocks?project="+projectID)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/blocks")
}

// SaveBlockContent 工作台保存块内容（POST /admin/blocks/save-content，JSON）。
// 工作台块编辑的唯一保存入口：保存后编排 stale（绑定该块的主题下页面标待重建）。
// 直连 REST /api/block/update 不做传播，供纯数据管理场景使用。
func (h *Handle) SaveBlockContent(c *gin.Context) {
	var req struct {
		ID       string          `json:"id" binding:"required"`
		Name     string          `json:"name"`
		Document json.RawMessage `json:"document"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(http.StatusBadRequest, "参数不合法")
		return
	}
	// 名称取现值（工作台只改文档）。
	current, err := h.blocks.Detail(c.Request.Context(), &blockdto.DetailReq{ID: req.ID})
	if err != nil || current == nil {
		c.String(http.StatusNotFound, "全局块不存在")
		return
	}
	name := current.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	if _, err := h.blocks.Update(c.Request.Context(), &blockdto.UpdateReq{
		ID: req.ID, Name: name, Document: req.Document,
	}); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	h.markStaleForBlock(c, req.ID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "已保存，关联页面将标记为待重建"})
}

// markStaleForBlock 全局块内容变更/删除后的传播：
// 反查页眉/页脚槽位绑定该块的主题（project 契约），逐主题标记页面待重建（page 契约）。
func (h *Handle) markStaleForBlock(c *gin.Context, blockID string) {
	themes, err := h.projects.ListThemesByBlockID(c.Request.Context(), blockID)
	if err != nil {
		return
	}
	for _, t := range themes {
		if err := h.pages.MarkStaleForTheme(c.Request.Context(), t.ID); err != nil {
			return
		}
	}
}
