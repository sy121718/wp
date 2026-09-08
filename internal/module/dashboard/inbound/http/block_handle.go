package dashboardhttp

// 全局块管理页（后台「全局块」入口）：页眉/页脚/区块的列表、新建与删除。
// 块内容编辑复用工作台（/workbench?block=ID）；块保存后的 stale 传播在本模块编排：
// block 模块不感知主题/页面，跨模块组合只能依赖双方契约（模块表隔离规范）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// blockRow 全局块列表行投影。
type blockRow struct {
	ID            string
	Name          string
	Kind          string
	KindLabel     string
	ReuseMode     string
	ReuseModeLabel string
	UpdatedAt     string
}

// blocksPageData 全局块管理页数据。
type blocksPageData struct {
	Title             string
	Menu              string
	Projects          []projectcontract.ProjectResp
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

// kindLabels 块类型中文标签（docs/02-D §4/§5 全量白名单）。
var kindLabels = map[string]string{
	"header": "页眉", "footer": "页脚", "block": "区块",
	"announcement": "公告栏", "sidebar": "侧边栏", "breadcrumb": "面包屑", "drawer": "抽屉导航", "search": "搜索框",
	"cta": "CTA 段", "trust": "信任徽章", "brands": "品牌墙", "contact": "联系方式", "about": "关于我们",
	"banner": "横幅", "grid": "多栏布局", "snippet": "片段模板",
}

func kindLabel(kind string) string {
	if label, ok := kindLabels[kind]; ok {
		return label
	}
	return "区块"
}

// reuseModeLabel 复用方式标签（docs/02-D §5）。
func reuseModeLabel(mode string) string {
	if mode == "template" {
		return "复制"
	}
	return "引用"
}

// toBlockRows 按 kind 精确过滤（header/footer 页眉页脚组）。
func toBlockRows(blocks []blockcontract.BlockResp, kind string) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind != kind {
			continue
		}
		rows = append(rows, toBlockRow(b))
	}
	return rows
}

// toOtherBlockRows 其余全部类型（新 kind + snippet 片段模板）归入「区块/复用资产」组。
func toOtherBlockRows(blocks []blockcontract.BlockResp) []blockRow {
	rows := make([]blockRow, 0, len(blocks))
	for _, b := range blocks {
		if b.Kind == "header" || b.Kind == "footer" {
			continue
		}
		rows = append(rows, toBlockRow(b))
	}
	return rows
}

func toBlockRow(b blockcontract.BlockResp) blockRow {
	return blockRow{
		ID: b.ID, Name: b.Name, Kind: b.Kind, KindLabel: kindLabel(b.Kind),
		ReuseMode: b.ReuseMode, ReuseModeLabel: reuseModeLabel(b.ReuseMode),
		UpdatedAt: b.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

// BlocksList 全局块管理页（GET /admin/blocks?project=X）。
func (h *Handle) BlocksList(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
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
		blocks, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: data.SelectedProjectID})
		if err != nil {
			response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
			return
		}
		data.Headers = toBlockRows(blocks, "header")
		data.Footers = toBlockRows(blocks, "footer")
		data.Blocks = toOtherBlockRows(blocks)
	}
	c.HTML(http.StatusOK, "admin/blocks", withCSRF(c, data.templateMap()))
}

// CreateBlock 新建全局块（POST /admin/blocks/create），成功后进工作台编辑内容。
func (h *Handle) CreateBlock(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	name := strings.TrimSpace(c.PostForm("name"))
	kind := strings.TrimSpace(c.PostForm("kind"))
	reuseMode := strings.TrimSpace(c.PostForm("reuseMode"))
	if projectID == "" || name == "" {
		c.String(http.StatusBadRequest, "工程与块名称不能为空")
		return
	}
	block, err := h.blocks.Create(c.Request.Context(), &blockcontract.CreateReq{
		ProjectID: projectID, Name: name, Kind: kind, ReuseMode: reuseMode,
	})
	if err != nil {
		logger.Scene("dashboard").With("op", "CreateBlock").Error(err, "创建全局块失败")
		c.String(http.StatusBadRequest, blockBizError(err))
		return
	}
	c.Redirect(http.StatusSeeOther, "/workbench?block="+block.ID)
}

// DeleteBlock 删除全局块（POST /admin/blocks/delete）。
// 删除后由 block service 统一编排 stale：绑定该块的主题下全部页面标待重建
// （产物退化为无页眉/页脚）。
func (h *Handle) DeleteBlock(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少块 id")
		return
	}
	force := strings.TrimSpace(c.PostForm("force")) == "1"
	if err := h.blocks.Delete(c.Request.Context(), &blockcontract.DeleteReq{ID: id, Force: force}); err != nil {
		logger.Scene("dashboard").With("op", "DeleteBlock").Error(err, "删除全局块失败")
		c.String(http.StatusBadRequest, blockBizError(err))
		return
	}
	if projectID != "" {
		c.Redirect(http.StatusSeeOther, "/admin/blocks?project="+projectID)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/blocks")
}

// SaveBlockContent 工作台保存块内容（POST /admin/blocks/save-content，JSON）。
// 工作台块编辑的保存入口：保存后由 block service 统一编排 stale
// （传播器已在装配时注入，REST /api/block/update 与 dashboard 保存路径同源传播）。
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
	current, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: req.ID})
	if err != nil || current == nil {
		c.String(http.StatusNotFound, "全局块不存在")
		return
	}
	name := current.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	if _, err := h.blocks.Update(c.Request.Context(), &blockcontract.UpdateReq{
		ID: req.ID, Name: name, Document: req.Document,
	}); err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "已保存，关联页面将标记为待重建"})
}

// markStaleForBlockCtx 全局块内容变更/删除后的 stale 传播（实现 block service 传播器契约），
// 覆盖三条引用路径：
//  1. 页眉/页脚槽位绑定该块的主题（project 契约 ListThemesByBlockID）→ 逐主题 MarkStaleForTheme；
//  2. core.globalref 引用（页面文档树内 "blockId" 节点）→ MarkStaleForBlock；
//  3. 页面级 settings.structure 页眉/页脚自选覆盖（headerBlockId/footerBlockId）→ MarkStaleForBlock。
//
// 路径 2/3 由 page 契约按 blockID 反查；路径 1 与 2/3 可能重叠命中同一页面，stale=true 幂等，无妨。
func (h *Handle) markStaleForBlockCtx(ctx context.Context, blockID string) error {
	themes, err := h.projects.ListThemesByBlockID(ctx, blockID)
	if err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "反查绑定块的主题失败")
		return err
	}
	for _, t := range themes {
		if err := h.pages.MarkStaleForTheme(ctx, t.ID); err != nil {
			logger.Scene("block").With("block_id", blockID).With("theme_id", t.ID).Error(err, "标记页面待重建失败")
			return err
		}
	}
	if err := h.pages.MarkStaleForBlock(ctx, blockID); err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "反查引用块页面标待重建失败")
		return err
	}
	return nil
}

// blockReferencedCtx 判断块是否仍被引用（block service 引用检查器契约，docs/02-D §9）：
// 主题页眉/页脚槽位绑定该块，或存在 globalref / settings.structure 自选引用页面。
func (h *Handle) blockReferencedCtx(ctx context.Context, blockID string) (bool, error) {
	themes, err := h.projects.ListThemesByBlockID(ctx, blockID)
	if err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "反查绑定块的主题失败")
		return false, err
	}
	if len(themes) > 0 {
		return true, nil
	}
	count, err := h.pages.CountBlockReference(ctx, blockID)
	if err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "统计引用块页面失败")
		return false, err
	}
	return count > 0, nil
}

// blockBizError 把 block 业务错误映射为用户可见文案；非业务错误（基础设施故障）回退兜底文案，
// 原文仅进日志不外泄（对齐「不直出 err.Error()」约定）。
func blockBizError(err error) string {
	switch {
	case errors.Is(err, blockcontract.ErrParamRequired),
		errors.Is(err, blockcontract.ErrNotFound),
		errors.Is(err, blockcontract.ErrProjectNotFound),
		errors.Is(err, blockcontract.ErrNameRequired),
		errors.Is(err, blockcontract.ErrInvalidDoc),
		errors.Is(err, blockcontract.ErrInvalidKind),
		errors.Is(err, blockcontract.ErrInvalidCategory),
		errors.Is(err, blockcontract.ErrDuplicate),
		errors.Is(err, blockcontract.ErrInvalidReuseMode),
		errors.Is(err, blockcontract.ErrBlockInUse):
		return err.Error()
	default:
		return dashboardenums.MsgInternalError
	}
}
