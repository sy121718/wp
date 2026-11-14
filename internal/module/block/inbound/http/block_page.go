package blockhttp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const blocksPageTitle = "MsgBlocksTitle"

type blockPageHandle struct {
	blocks   blockcontract.BlockService
	projects projectcontract.ProjectService

	pages pagecontract.PageService
}

func NewBlockPageHandle(blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) *blockPageHandle {
	h := &blockPageHandle{blocks: blocks, projects: projects}
	if len(pages) > 0 {
		h.pages = pages[0]
	}
	return h
}

type blockRow struct {
	ID        string
	Name      string
	Kind      string
	ReuseMode string
	UpdatedAt utils.JSONTime

	RefCount int64
}

type blocksPageData struct {
	Title    string
	Menu     string
	Projects []projectcontract.ProjectResp

	SelectedProject string
	Headers         []blockRow
	Footers         []blockRow
	Blocks          []blockRow

	StaleImpact staleImpactView
}

type stalePageRow struct {
	ID          string
	Path        string
	ProjectID   string
	ProjectName string
}

type staleImpactView struct {
	Available bool
	Pages     []stalePageRow
	Total     int
	Truncated bool
	Limit     int

	Hint string
}

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
		ID: b.ID, Name: b.Name, Kind: b.Kind,
		ReuseMode: b.ReuseMode, UpdatedAt: b.UpdatedAt,
	}
}

func (h *blockPageHandle) BlocksList(c *gin.Context) {
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, response.InternalKey)
		return
	}
	data := &blocksPageData{
		Title:    blocksPageTitle,
		Menu:     "blocks",
		Projects: projects,
	}
	if sel := strings.TrimSpace(c.Query("project")); sel != "" {
		data.SelectedProject = sel
	} else if len(projects) > 0 {
		data.SelectedProject = projects[0].ID
	}
	if data.SelectedProject != "" {
		blocks, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: data.SelectedProject})
		if err != nil {
			response.ErrorWithMessage(c, http.StatusInternalServerError, response.InternalKey)
			return
		}
		data.Headers = toBlockRows(blocks, "header")
		data.Footers = toBlockRows(blocks, "footer")
		data.Blocks = toOtherBlockRows(blocks)

		h.fillRefCounts(c.Request.Context(), data.Headers)
		h.fillRefCounts(c.Request.Context(), data.Footers)
		h.fillRefCounts(c.Request.Context(), data.Blocks)
	}

	data.StaleImpact = h.blockStaleImpact(c, c.Request.Context())

	pageview.Render(c, data)
}

func (h *blockPageHandle) SaveBlockContent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseJSON[blockdto.SaveContentReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, blockErrScene, blockenums.Err(blockenums.ErrBlockParamRequired))
		return
	}

	current, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: req.ID})
	if err != nil {
		pageview.FailScene(c, blockErrScene, err)
		return
	}
	if current == nil {
		pageview.FailStatus(c, http.StatusNotFound, blockErrScene, blockenums.Err(blockenums.ErrBlockNotFound))
		return
	}
	name := current.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	if _, uerr := h.blocks.Update(c.Request.Context(), &blockcontract.UpdateReq{
		ID: req.ID, Name: name, Document: req.Document, ExpectedVersion: req.ExpectedVersion,
	}); uerr != nil {
		pageview.FailScene(c, blockErrScene, uerr)
		return
	}

	c.HTML(http.StatusOK, "admin/block/block_content_saved.html", pageview.Objects(
		pageview.Object("Name", name),
	))
}

const blockErrScene = "block"

const blockStalePageLimit = 8

var (
	blockImpactNoPageCapabilityMsg = facing.Msg{
		Key:      blockenums.ImpactUnavailableNoPageCapability,
		Fallback: "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。",
	}
	blockImpactProjectReadFailedMsg = facing.Msg{
		Key:      blockenums.ImpactUnavailableProjectReadFailed,
		Fallback: "读取站点工程失败，本次无法统计待重建影响面。",
	}
)

func (h *blockPageHandle) blockStaleImpact(c *gin.Context, ctx context.Context) staleImpactView {

	unavailable := func(msg facing.Msg) staleImpactView {
		return staleImpactView{
			Available: false, Pages: []stalePageRow{}, Total: 0, Truncated: false,
			Limit: blockStalePageLimit, Hint: response.MsgText(c, msg),
		}
	}
	if h == nil || h.pages == nil {
		return unavailable(blockImpactNoPageCapabilityMsg)
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		Limit:      blockStalePageLimit,
		Descending: true,
	})
	if err != nil {

		logger.Scene("block").Error(err, "读取全站待重建清单失败，待重建影响面本次不可用")
		return unavailable(blockImpactProjectReadFailedMsg)
	}
	if res == nil {
		logger.Scene("block").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable(facing.Msg{})
	}
	pages := make([]stalePageRow, 0, len(res.Pages))
	for i := range res.Pages {
		pages = append(pages, stalePageRow{
			ID:          res.Pages[i].ID,
			Path:        res.Pages[i].Path,
			ProjectID:   res.Pages[i].ProjectID,
			ProjectName: res.Pages[i].ProjectName,
		})
	}
	return staleImpactView{
		Available: true, Pages: pages, Total: res.Total, Truncated: res.Truncated,
		Limit: blockStalePageLimit, Hint: "",
	}
}

func (h *blockPageHandle) fillRefCounts(ctx context.Context, rows []blockRow) {
	for i := range rows {
		rows[i].RefCount = h.blockRefCount(ctx, rows[i])
	}
}

func (h *blockPageHandle) blockRefCount(ctx context.Context, b blockRow) int64 {
	if h == nil || h.pages == nil {
		return -1
	}
	if b.ReuseMode != blockmodel.ReuseGlobal {
		return -1
	}
	n, err := h.pages.CountBlockReference(ctx, b.ID)
	if err != nil {
		logger.Scene("block").With("block_id", b.ID).Error(err, "统计块引用页面数失败（影响面显示为未知）")
		return -1
	}
	return n
}

func (h *blockPageHandle) BlocksStaleDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.pages == nil {
		c.Status(http.StatusNotFound)
		return
	}
	res, err := h.pages.ListStalePages(c.Request.Context(), &pagecontract.StalePageListReq{
		Limit:      blockStalePageLimit,
		Descending: true,
	})
	if err != nil {
		logger.Scene("block").Error(err, "读取全站待重建清单失败（抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	if res == nil {
		logger.Scene("block").Warn("全站待重建清单返回空结果（契约实现异常，抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	rows := make([]staleDrawerRow, 0, len(res.Pages))
	projects := make(map[string]struct{}, 2)
	for i := range res.Pages {
		projects[res.Pages[i].ProjectID] = struct{}{}
		rows = append(rows, staleDrawerRow{
			ID:          res.Pages[i].ID,
			Path:        res.Pages[i].Path,
			ProjectName: res.Pages[i].ProjectName,
			Published:   res.Pages[i].Published,

			RebuildFailure: rebuildFailureViewOf(res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}

	pageview.Render(c, stalePagesDrawerView{
		Rows: rows, Total: res.Total, Truncated: res.Truncated,
		Limit: blockStalePageLimit, MultiProject: len(projects) > 1,
	})
}

type staleDrawerRow struct {
	ID             string
	Path           string
	ProjectName    string
	Published      bool
	RebuildFailure rebuildFailureView
}

type stalePagesDrawerView struct {
	Rows         []staleDrawerRow
	Total        int
	Truncated    bool
	Limit        int
	MultiProject bool
}

type rebuildFailureView struct {
	StageKey      string
	StageFallback string

	At string

	Has bool
}

func rebuildFailureViewOf(stage string, at *utils.JSONTime) rebuildFailureView {
	if strings.TrimSpace(stage) == "" && at == nil {
		return rebuildFailureView{}
	}
	view := rebuildFailureView{Has: true, StageFallback: stage}
	switch stage {
	case "plan":
		view.StageKey, view.StageFallback = blockenums.ImpactStagePlan, "计划阶段（站点语言清单或旧发布范围读不到）"
	case "build":
		view.StageKey, view.StageFallback = blockenums.ImpactStageBuild, "构建 / 发布阶段"
	}
	if at != nil {
		view.At = time.Time(*at).Local().Format("2006-01-02 15:04")
	}
	return view
}
