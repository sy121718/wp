package blockhttp

// block_page_handle.go — 全局块管理页（后台「全局块」入口）：页眉/页脚/区块的列表、
// 新建与删除。自 dashboard 迁回本模块；块内容编辑复用工作台（/workbench?block=ID）。
// stale 传播 / 引用检查已由装配层接管（internal/routers/block_page_bridge.go），
// 本页不再持有任何跨模块失效编排。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// blocksPageTitle 全局块管理页标题（i18n key，与原 dashboard 枚举同值）。
const blocksPageTitle = "MsgBlocksTitle"

// blockPageHandle 全局块管理页处理器。
type blockPageHandle struct {
	blocks   blockcontract.BlockService
	projects projectcontract.ProjectService
}

// NewBlockPageHandle 创建全局块管理页处理器。
func NewBlockPageHandle(blocks blockcontract.BlockService, projects projectcontract.ProjectService) *blockPageHandle {
	return &blockPageHandle{blocks: blocks, projects: projects}
}

// blockRow 全局块列表行投影。
type blockRow struct {
	ID             string
	Name           string
	Kind           string
	KindLabel      string
	ReuseMode      string
	ReuseModeLabel string
	UpdatedAt      string
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
	// Err / Done 上一次写动作的回执（删除结果、部分失败计数）。本页此前没有任何回执通道，
	// 删除成不成功在页面上看不出来 —— 批量动作必须把「跳过几个」说清楚。
	Err  string
	Done string
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
		"Err":             d.Err,
		"Done":            d.Done,
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
		UpdatedAt: b.UpdatedAt.Time().Format("2006-01-02 15:04"),
	}
}

// BlocksList 全局块管理页（GET /admin/blocks?project=X）。
func (h *blockPageHandle) BlocksList(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	data := &blocksPageData{
		Title:    blocksPageTitle,
		Menu:     "blocks",
		Projects: projects,
		Err:      strings.TrimSpace(c.Query("err")),
		Done:     strings.TrimSpace(c.Query("done")),
	}
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProjectID = sel
	} else if len(projects) > 0 {
		data.SelectedProjectID = projects[0].ID
	}
	if data.SelectedProjectID != "" {
		blocks, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: data.SelectedProjectID})
		if err != nil {
			response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
			return
		}
		data.Headers = toBlockRows(blocks, "header")
		data.Footers = toBlockRows(blocks, "footer")
		data.Blocks = toOtherBlockRows(blocks)
	}
	c.HTML(http.StatusOK, "admin/blocks", shell.Prepare(c, data.templateMap()))
}

// CreateBlock 新建全局块（POST /admin/blocks/create），成功后进工作台编辑内容。
func (h *blockPageHandle) CreateBlock(c *gin.Context) {
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
		logger.Scene("block").With("op", "CreateBlock").Error(err, "创建全局块失败")
		c.String(http.StatusBadRequest, blockBizError(err))
		return
	}
	c.Redirect(http.StatusSeeOther, "/workbench?block="+block.ID)
}

// DeleteBlock 删除全局块（POST /admin/blocks/delete）。
// 删除后由 block service 统一编排 stale（传播器在装配层注入）：绑定该块的主题下
// 全部页面标待重建（产物退化为无页眉/页脚）。
func (h *blockPageHandle) DeleteBlock(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	if id == "" {
		c.String(http.StatusBadRequest, "缺少块 id")
		return
	}
	force := strings.TrimSpace(c.PostForm("force")) == "1"
	if err := h.blocks.Delete(c.Request.Context(), &blockcontract.DeleteReq{ID: id, Force: force}); err != nil {
		logger.Scene("block").With("op", "DeleteBlock").Error(err, "删除全局块失败")
		c.String(http.StatusBadRequest, blockBizError(err))
		return
	}
	if projectID != "" {
		c.Redirect(http.StatusSeeOther, "/admin/blocks?project="+projectID)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/blocks")
}

// BlocksBulkDelete 批量删除全局块（POST /admin/blocks/bulk-delete，权限点 block:delete）。
//
// 逐条走**同一条单条删除路径**（h.blocks.Delete）：被页面引用的全局块由服务端拒绝，
// 其余照常删除 —— 单条失败不中断整批（整批回滚会让用户以为「一个都没删」然后反复重试）。
// 结果按「已删除 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *blockPageHandle) BlocksBulkDelete(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		q := url.Values{}
		if projectID != "" {
			q.Set("project", projectID)
		}
		q.Set("err", berr.Error())
		c.Redirect(http.StatusSeeOther, "/admin/blocks?"+q.Encode())
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.blocks.Delete(c.Request.Context(), &blockcontract.DeleteReq{ID: id}); err != nil {
			logger.Scene("block").With("op", "BlocksBulkDelete").Error(err, "批量删除全局块失败")
			skipped++
			continue
		}
		deleted++
	}
	q := url.Values{}
	if projectID != "" {
		q.Set("project", projectID)
	}
	// 有跳过就进 ?err=（警告条更显眼，用户下次会去看剩下那些）；全成功才进 ?done=。
	if msg := blocksBulkDeleteResult(deleted, skipped); msg != "" {
		if skipped > 0 {
			q.Set("err", msg)
		} else {
			q.Set("done", msg)
		}
	}
	target := "/admin/blocks"
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	c.Redirect(http.StatusSeeOther, target)
}

// blocksBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func blocksBulkDeleteResult(deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return "没有选中任何块，列表未改动。"
	case skipped == 0:
		return fmt.Sprintf("已删除 %d 个块。", deleted)
	case deleted == 0:
		return fmt.Sprintf("%d 个块都未能删除，列表未改动。", skipped)
	default:
		return fmt.Sprintf("已删除 %d 个，%d 个未能删除（被页面引用的全局块需先解除引用）。", deleted, skipped)
	}
}

// SaveBlockContent 工作台保存块内容（POST /admin/blocks/save-content，JSON）。
// 工作台块编辑的保存入口：保存后由 block service 统一编排 stale
// （传播器已在装配层注入，REST /api/block/update 与本保存路径同源传播）。
func (h *blockPageHandle) SaveBlockContent(c *gin.Context) {
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
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "已保存，关联页面将标记为待重建"})
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
		return shell.MsgInternalError
	}
}

// SetupBlockPages 注册全局块管理页（/admin 组，中间件链由装配层统一挂好）。
// 函数名沿用 SetupXxxPages 先例：本包已有 REST 路由的 SetupBlockRoutes，不能同名。
// adminPages 为 nil 时整体跳过。
func SetupBlockPages(adminPages *gin.RouterGroup,
	blocks blockcontract.BlockService, projects projectcontract.ProjectService) {
	if adminPages == nil {
		return
	}
	h := NewBlockPageHandle(blocks, projects)
	adminPages.GET("/blocks", h.BlocksList)
	adminPages.POST("/blocks/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.CreateBlock)
	adminPages.POST("/blocks/delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), h.DeleteBlock)
	// 批量删除复用单条删除的权限点（不新增权限点、不写迁移）：能删一个块的人就能删一批。
	adminPages.POST("/blocks/bulk-delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), h.BlocksBulkDelete)
	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), h.SaveBlockContent)
}
