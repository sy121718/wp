package pagehttp

import (
	"context"
	"errors"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/shell"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

type pagesAdminHandle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService

	blocks blockcontract.BlockService

	blueprints blueprintcontract.BlueprintService

	siteIndex siteContentIndexCache

	contentStore contentTranslationPort
}

func NewPagesAdminHandle(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, blueprints blueprintcontract.BlueprintService) *pagesAdminHandle {
	return &pagesAdminHandle{pages: pages, projects: projects, blocks: blocks, blueprints: blueprints}
}

type PagesAdminHandle = pagesAdminHandle

func (h *pagesAdminHandle) SetBlueprints(b blueprintcontract.BlueprintService) { h.blueprints = b }

func (h *pagesAdminHandle) blueprintOptions(ctx context.Context) []blueprintOption {
	if h.blueprints == nil {
		return nil
	}
	list, err := h.blueprints.List(ctx, &blueprintdto.ListReq{})
	if err != nil {
		logger.Scene("page").With("err", err).Warn("蓝图列表读取失败，新建页面表单不显示蓝图选项")
		return nil
	}
	out := make([]blueprintOption, 0, len(list))
	for _, b := range list {
		out = append(out, blueprintOption{ID: b.ID, Name: b.Name})
	}
	return out
}

type pagesPageData struct {
	Title    string `view:"title"`
	Menu     string `view:"menu"`
	Projects []projectcontract.ProjectResp
	Pages    []PageRowResp

	Blueprints []blueprintOption

	Err pageMsg

	ReceiptPending int64
	ReceiptOldest  string

	ReceiptLastConverge *utils.JSONTime
	ReceiptAlert        bool

	ReceiptKnown bool

	SelectedProject string

	FilteredProject bool

	LangOptions []PageListLangOptionResp

	LangStateOptions []PageListStateOptionResp

	SelectedLang string

	FilteredLang bool

	StaleOverview *StaleOverviewResp
}

type blueprintOption struct {
	ID   string
	Name string
}

func (h *pagesAdminHandle) PagesList(c *gin.Context) {
	data, err := h.buildPagesData(c)
	if err != nil {
		data = &pagesPageData{
			Title: pageenums.MsgPagesTitle, Menu: "pages",

			Err: pageErrMsg(c, err),
		}
	}
	pageview.RenderTemplate(c, PagesListPageTemplate, data)
}

func (h *pagesAdminHandle) buildPagesData(c *gin.Context) (*pagesPageData, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}

	filter, ferr := binding.ParseQuery[pagedto.PageListQueryReq](c)
	if ferr != nil {
		logger.Scene("page").With("err", ferr).Warn("页面列表筛选参数解析失败，按无筛选渲染")
		filter = &pagedto.PageListQueryReq{}
	}

	projectID, filteredProject := focusProjectID(projects, filter.Project)

	themeID := ""
	if projectID != "" {
		if theme, err := h.projects.GetActiveTheme(ctx, projectID); err == nil && theme != nil {
			themeID = theme.ID
		}
	}

	pages, err := h.pages.List(ctx, &pagecontract.ListReq{
		ProjectID: projectID, ThemeID: themeID,
		Lang: filter.Lang, LangState: filter.State,
	})
	if err != nil {
		return nil, err
	}
	rows := h.pageRowsOf(pages)

	langOptions, langStates, selectedLang := pageListLangFilter(h.enabledLangsOf(ctx, projectID), filter.Lang, filter.State)

	receiptPending, receiptOldest, receiptLast := h.receiptBacklog(ctx)

	stale := h.staleOverview(ctx)
	return &pagesPageData{
		Title: pageenums.MsgPagesTitle, Menu: "pages",
		Projects: projects, Pages: rows,

		SelectedProject: projectID,
		FilteredProject: filteredProject,

		LangOptions:      langOptions,
		LangStateOptions: langStates,
		SelectedLang:     selectedLang,
		FilteredLang:     selectedLang != "",

		Blueprints: h.blueprintOptions(ctx),

		ReceiptPending:      receiptPending,
		ReceiptOldest:       receiptOldest,
		ReceiptLastConverge: receiptLast,
		ReceiptAlert:        receiptPending > 0,

		ReceiptKnown: true,

		StaleOverview: stale,
	}, nil
}

func (h *pagesAdminHandle) pageRowsOf(pages []pagedto.PageResp) []PageRowResp {
	rows := make([]PageRowResp, 0, len(pages))
	for _, p := range pages {
		row := PageRowResp{
			ID: p.ID, ProjectID: p.ProjectID, Kind: p.Kind,
			DraftPath: p.DraftPath, Publications: p.Publications, Active: p.ActiveArtifactID != nil,
			Staged: p.StagedArtifactID != nil, Stale: p.Stale,
			Version: p.DraftVersion, UpdatedAt: p.UpdatedAt,
		}
		if p.ActivePath != nil {
			row.ActivePath = *p.ActivePath
		}

		if p.LangStatus != "" {
			row.LangScope = p.LangStatus
			row.LangStatusKey, row.LangStatusFb = pageenums.LangStatusLabel(p.LangStatus)
		}
		rows = append(rows, row)
	}
	return rows
}

func pageListLangFilter(langs []string, wantLang, wantState string) (options []PageListLangOptionResp, states []PageListStateOptionResp, selected string) {
	langs = dedupeLangs(langs)
	if len(langs) <= 1 {
		return nil, nil, ""
	}
	defaultLang := i18n.GetDefaultLang()

	wantLang = strings.TrimSpace(wantLang)
	for i := range langs {
		if strings.EqualFold(langs[i], wantLang) {
			selected = langs[i]
			break
		}
	}
	options = make([]PageListLangOptionResp, 0, len(langs))
	for i := range langs {
		options = append(options, PageListLangOptionResp{
			Value: langs[i], Label: langs[i],
			Default: langs[i] == defaultLang, Active: langs[i] == selected,
		})
	}
	states = pageListStateOptions(wantState)
	return options, states, selected
}

func pageListStateOptions(want string) []PageListStateOptionResp {
	active := pageenums.LangStatusParse(want, pageenums.LangStatusPublished)
	order := []string{pageenums.LangStatusPublished, pageenums.LangStatusDraft, pageenums.LangStatusExcluded}
	out := make([]PageListStateOptionResp, 0, len(order))
	for _, status := range order {
		key, fallback := pageenums.LangStatusLabel(status)
		out = append(out, PageListStateOptionResp{
			Value: status, Key: key, Fallback: fallback, Active: status == active,
		})
	}
	return out
}

func dedupeLangs(langs []string) []string {
	seen := make(map[string]bool, len(langs))
	out := make([]string, 0, len(langs))
	for _, lang := range langs {
		lang = strings.TrimSpace(lang)
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		out = append(out, lang)
	}
	return out
}

func focusProjectID(projects []projectcontract.ProjectResp, want string) (id string, filtered bool) {
	want = strings.TrimSpace(want)
	if want != "" {
		for i := range projects {
			if projects[i].ID == want {
				return projects[i].ID, true
			}
		}
	}
	if len(projects) == 0 {
		return "", false
	}
	return projects[0].ID, false
}

func (h *pagesAdminHandle) CreateProject(c *gin.Context) {
	req, ok := binding.Form[pagedto.ProjectCreateFormReq](c)
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		pageListJump(c, false, pagesLocalNoticeProjectNameRequired)
		return
	}
	if _, err := h.projects.Create(c.Request.Context(), &projectcontract.CreateReq{
		Name: name, Settings: json.RawMessage("{}"),
	}); err != nil {
		logger.Scene("page").With("name", name).Error(err, "创建站点工程失败")

		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	pageListJump(c, true, pageProjectCreatedMsg())
}

func (h *pagesAdminHandle) CreatePage(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageCreateFormReq](c)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	path := strings.TrimSpace(req.DraftPath)

	if projectID == "" {
		pageListJump(c, false, pageMsg{Key: pageenums.ErrProjectRequired, Fallback: pageenums.ErrProjectRequired})
		return
	}
	if path == "" {
		pageListJump(c, false, pagesLocalNoticePathRequired)
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if _, err := h.pages.Create(c.Request.Context(), &pagecontract.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         path,
		DraftDocument:     json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		BlueprintID:       strings.TrimSpace(req.BlueprintID),
	}); err != nil {
		logger.Scene("page").With("projectId", projectID).With("path", path).Error(err, "创建页面失败")
		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	pageListJump(c, true, pageCreatedMsg())
}

func (h *pagesAdminHandle) DeletePage(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageDeleteFormReq](c)
	if !ok {
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		pageListJump(c, false, pagesLocalNoticeMissingID)
		return
	}
	if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("pageId", id).Error(err, "删除页面失败")

		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	pageListJump(c, true, pageDeletedMsg())
}

func (h *pagesAdminHandle) PagesBulkDelete(c *gin.Context) {

	ids, berr := shell.BulkIDs(c)
	if berr != nil {

		pageListJump(c, false, pageBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
			logger.Scene("page").With("pageId", id).Error(err, "批量删除页面失败")
			skipped++
			continue
		}
		deleted++
	}
	pageListJump(c, skipped == 0 && deleted > 0, outcomeMsg(pageenums.PagesBulkDeleteOutcome(deleted, skipped)))
}

func pageProjectCreatedMsg() pageMsg {
	return pageMsg{Key: projectenums.MsgProjectCreated, Fallback: "站点工程创建成功"}
}

func pageCreatedMsg() pageMsg {
	return pageMsg{Key: pageenums.MsgPageCreated, Fallback: "页面创建成功"}
}

func pageDeletedMsg() pageMsg {
	return outcomeMsg(pageenums.PagesBulkDeleteOutcome(1, 0))
}

var (
	pagesLocalNoticeMissingID = pageMsg{Key: pageenums.BulkPageMissingID, Fallback: "缺少页面 id，未执行删除。"}

	pagesLocalNoticeProjectNameRequired = pageMsg{Key: pageenums.PageFormProjectNameRequired, Fallback: "站点工程名称不能为空，未创建。"}

	pagesLocalNoticePathRequired = pageMsg{Key: pageenums.PageFormPathRequired, Fallback: "页面路径不能为空，未创建。"}
)

func pageBulkIDsMsg(c *gin.Context, err error) pageMsg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {

		logger.Scene(pageErrScene).With("path", c.Request.URL.Path).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		return pageInternalMsg()
	}
	logger.Scene(pageErrScene).With("path", c.Request.URL.Path).
		With("count", tooMany.Count).With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return pageMsg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

type Handle struct {
	svc pagecontract.PageService
}

func NewHandle(svc pagecontract.PageService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) Create(c *gin.Context) {
	req, ok := binding.JSON[pagedto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageCreated, res)
}

func (h *Handle) Detail(c *gin.Context) {
	req, ok := binding.Query[pagedto.DetailReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDetail, res)
}

func (h *Handle) List(c *gin.Context) {
	req, ok := binding.Query[pagedto.ListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) SaveDraft(c *gin.Context) {
	req, ok := binding.JSON[pagedto.SaveDraftReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SaveDraft(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgDraftSaved, res)
}

func (h *Handle) Build(c *gin.Context) {
	req, ok := binding.JSON[pagedto.BuildReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Build(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgBuildReady, res)
}

func (h *Handle) Publish(c *gin.Context) {
	req, ok := binding.JSON[pagedto.PublishReq](c)
	if !ok {
		return
	}
	if req.AllLangs {

		res, err := h.svc.PublishAllLanguages(c.Request.Context(), req)
		if err != nil {
			response.ErrorStatusAuto(c, pageErrScene, err)
			return
		}
		response.SuccessWithMessage(c, pageenums.MsgPublished, res)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPublished, res)
}

func (h *Handle) RebuildArtifact(c *gin.Context) {
	req, ok := binding.JSON[pagedto.RebuildArtifactReq](c)
	if !ok {
		return
	}
	res, err := h.svc.RebuildArtifact(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) GarbageCollectArtifacts(c *gin.Context) {
	req, ok := binding.JSON[pagedto.GCArtifactsReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GarbageCollectArtifacts(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) SEOPatrol(c *gin.Context) {
	req, ok := binding.Query[pagedto.SEOPatrolReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SEOPatrol(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) AuditPublication(c *gin.Context) {
	res, err := h.svc.AuditPublication(c.Request.Context())
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) Rollback(c *gin.Context) {
	req, ok := binding.JSON[pagedto.RollbackReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Rollback(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRollbackDone, res)
}

func (h *Handle) UpdateURL(c *gin.Context) {
	req, ok := binding.JSON[pagedto.UpdateURLReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateURL(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgURLUpdated, res)
}

func (h *Handle) Delete(c *gin.Context) {
	req, ok := binding.JSON[pagedto.DeleteReq](c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgPageDeleted, nil)
}

func (h *Handle) ListRevisions(c *gin.Context) {
	req, ok := binding.Query[pagedto.RevisionReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListRevisions(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, pageErrScene, err)
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgRevisionsListed, res)
}

func (h *pagesAdminHandle) HistoryPanel(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageHistoryRestoreFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)

	view := &PageHistoryListResp{Revisions: []HistoryRowResp{}, Error: pageMsg{}}
	if pageID != "" && h.pages != nil {
		revs, err := h.pages.ListRevisions(c.Request.Context(), &pagecontract.RevisionReq{PageID: pageID})
		if err != nil {
			view.Error = pageMsg{Key: pageenums.HistoryLoadFailed, Fallback: "加载失败"}
		} else {
			views := make([]HistoryRowResp, 0, len(revs))
			for _, r := range revs {
				views = append(views, HistoryRowResp{
					Version: r.Version, Path: r.DraftPath,
					CreatedAt: r.CreatedAt,
				})
			}
			view.Revisions = views
		}
	}
	pageview.RenderTemplate(c, PageHistoryListTemplate, view)
}

func (h *pagesAdminHandle) HistoryRestore(c *gin.Context) {
	ctx := c.Request.Context()
	req, ok := binding.Form[pagedto.PageHistoryRestoreFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	version, err := strconv.ParseInt(strings.TrimSpace(req.Version), 10, 64)
	if pageID == "" || err != nil || h.pages == nil {

		response.ErrorWithMessage(c, http.StatusBadRequest, pageenums.ErrInvalidParam)
		return
	}
	revs, err := h.pages.ListRevisions(ctx, &pagecontract.RevisionReq{PageID: pageID})
	if err != nil {
		logger.Scene("page").With("page_id", pageID).Error(err, "读取页面修订失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, adminui.MsgInternalError)
		return
	}
	var target *pagecontract.RevisionResp
	for i := range revs {
		if revs[i].Version == version {
			target = &revs[i]
			break
		}
	}
	if target == nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pageenums.ErrRevisionNotFound)
		return
	}
	page, err := h.pageOf(c, pageID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pageenums.ErrPageNotFound)
		return
	}
	res, err := h.pages.SaveDraft(ctx, &pagecontract.SaveDraftReq{
		ID: pageID, ExpectedVersion: page.DraftVersion,
		DraftPath: target.DraftPath, DraftDocument: target.DraftDocument,
	})
	if err != nil {

		logger.Scene("page").With("page_id", pageID).Error(err, "恢复页面修订失败")
		response.ErrorWithMessage(c, http.StatusConflict, pageenums.ErrDraftVersionConflict)
		return
	}
	response.Success(c, gin.H{"draftVersion": res.DraftVersion})
}
