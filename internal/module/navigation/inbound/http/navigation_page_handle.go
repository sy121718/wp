package navigationhttp

import (
	"context"
	"errors"
	"go_wp/pkg/response"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	pageview "go_wp/pkg/view"
)

type navEditEcho struct {
	ID, ProjectID, Kind, Title, Path, Target, UpdatedAt string
	PanelBlockID, PanelWidth                            string
}

func (h *navigationPageHandle) NavigationEditFragment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("id"))
	projectID := strings.TrimSpace(c.Query("project"))
	kind := strings.TrimSpace(c.Query("kind"))
	if id == "" || projectID == "" || kind != normalizeNavKind(kind) {
		c.String(http.StatusBadRequest, "%s", response.MsgText(c, navInvalidParamMsg))
		return
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: id})
	if err != nil {
		if err.Error() == navigationenums.ErrNotFound {
			c.String(http.StatusNotFound, "%s", response.MsgText(c, navigationErrMsg(c, err)))
			return
		}
		logger.Scene("page").With("id", id).Error(err, "加载导航编辑片段失败")
		c.String(http.StatusInternalServerError, "%s", response.InternalText(c))
		return
	}
	if item == nil || item.ProjectID != projectID || item.Kind != kind {
		c.String(http.StatusNotFound, "%s", response.MsgText(c, navNoticeItemMissing))
		return
	}
	panelID := ""
	if item.PanelBlockID != nil {
		panelID = *item.PanelBlockID
	}
	h.renderNavigationEdit(c, navEditEcho{
		ID: item.ID, ProjectID: projectID, Kind: kind, Title: item.Title,
		Path: item.Path, Target: item.Target, UpdatedAt: item.UpdatedAt,
		PanelBlockID: panelID, PanelWidth: item.PanelWidth,
	}, navText{})
}

func (h *navigationPageHandle) renderNavigationEdit(c *gin.Context, echo navEditEcho, errMsg navText) {
	c.Header("Cache-Control", "no-store")
	blocks := make([]panelBlockOption, 0)
	panelAvail := h.blocks != nil
	if panelAvail {
		list, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: echo.ProjectID})
		if err != nil {
			logger.Scene("page").With("project", echo.ProjectID).Error(err, "加载导航面板块候选失败")
			panelAvail = false
		} else {
			for _, b := range list {
				blocks = append(blocks, panelBlockOption{ID: b.ID, Name: b.Name})
			}
		}
	}

	data := &navigationEditFormView{
		NavEditEcho:        echo,
		NavEditError:       response.MsgText(c, errMsg),
		NavEditPanelAvail:  panelAvail,
		NavEditPanelBlocks: blocks,
	}

	token, err := builtin.GetCSRFToken(c)
	if err != nil || token == "" {
		logger.Scene("page").With("id", echo.ID).Error(err, "加载导航编辑 CSRF token 失败")
		c.String(http.StatusInternalServerError, "%s", response.InternalText(c))
		return
	}
	data.CSRFToken = token
	pageview.Render(c, data)
}

func navEditHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

func (h *navigationPageHandle) navEditFailure(c *gin.Context, err error, echo navEditEcho) {
	if !navEditHTMX(c) {

		navJumpListMenu(c, false, navigationErrMsg(c, err))
		return
	}
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {

		c.Header("HX-Redirect", navListBack(c))
		c.Status(http.StatusOK)
		return
	}

	h.renderNavigationEdit(c, echo, navigationErrMsg(c, err))
}

func (h *navigationPageHandle) navEditOwnedRow(c *gin.Context, echo navEditEcho) bool {
	if echo.ID == "" || echo.ProjectID == "" || echo.Kind != normalizeNavKind(echo.Kind) {
		c.String(http.StatusBadRequest, "%s", response.MsgText(c, navInvalidParamMsg))
		return false
	}
	item, err := h.navigations.Get(c.Request.Context(), &navigationdto.GetReq{ID: echo.ID})
	if err != nil && err.Error() != navigationenums.ErrNotFound {
		logger.Scene("page").With("id", echo.ID).Error(err, "核对导航编辑归属失败")
		c.String(http.StatusInternalServerError, "%s", response.InternalText(c))
		return false
	}
	if item == nil || item.ProjectID != echo.ProjectID || item.Kind != echo.Kind {
		c.String(http.StatusNotFound, "%s", response.MsgText(c, navNoticeItemMissing))
		return false
	}
	return true
}

const (
	navigationsPageTitle = "MsgNavigationsTitle"
)

const (
	navKindHeader       = "header"
	navKindHeaderMobile = "header_mobile"
	navKindFooter       = "footer"
	navKindFooterMobile = "footer_mobile"
)

type navigationPageHandle struct {
	navigations navigationcontract.NavigationService
	projects    projectcontract.ProjectService

	blocks BlockPanelPort
}

func NewNavigationPageHandle(navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService) *navigationPageHandle {
	return &navigationPageHandle{navigations: navigations, projects: projects}
}

type navMenuRow struct {
	ID    string
	Title string
	Path  string

	Depth int

	Target     string
	SourceType string
	First      bool
	Last       bool

	PanelBlockID string
	PanelWidth   string

	UpdatedAt string
}

type navSourceGroup struct {
	Type string

	Title string
	Items []navigationcontract.SourceCandidate

	Usable bool
}

type panelBlockOption struct {
	ID   string
	Name string
}

type navigationsPageData struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Kind            string
	Rows            []navMenuRow

	ParentOptions []navMenuRow

	SourceGroups []navSourceGroup

	MenuID string

	LoadError string
}

func (h *navigationPageHandle) NavigationsPage(c *gin.Context) {
	data, err := h.buildNavigationsData(c)
	if err != nil {

		logger.Scene("page").With("path", c.Request.URL.Path).Error(err, "加载导航菜单页失败")

		pageview.Fail(c, err)
		return
	}
	pageview.Render(c, data)
}

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

		if groups, terr = h.navigations.SourceGroups(ctx, selected); terr != nil {
			logger.Scene("page").With("project", selected).Warn("导航来源候选加载失败，管理页仅显示自定义链接")
		}
	}

	sourceGroups := make([]navSourceGroup, 0, len(groups))
	for _, g := range groups {
		usable := false
		for _, it := range g.Items {
			if strings.TrimSpace(it.URL) != "" {
				usable = true
				break
			}
		}
		sourceGroups = append(sourceGroups, navSourceGroup{Type: g.Type, Title: g.Title, Items: g.Items, Usable: usable})
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
		Rows: rows, ParentOptions: parents, SourceGroups: sourceGroups,

		MenuID: normalizeMenuFocus(c.Query("menu")),
	}, nil
}

func normalizeMenuFocus(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || len(v) > 64 {
		return ""
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return ""
		}
	}
	return v
}

func flattenNavRows(nodes []*navigationdto.NavigationNode, depth int, out *[]navMenuRow) {
	for i, n := range nodes {
		*out = append(*out, navMenuRow{
			ID: n.ID, Title: n.Title, Path: n.Path, Depth: depth,
			Target: n.Target, SourceType: n.SourceType,
			First: i == 0, Last: i == len(nodes)-1,
			PanelBlockID: panelBlockIDOf(n), PanelWidth: n.PanelWidth,
			UpdatedAt: n.UpdatedAt,
		})
		flattenNavRows(n.Children, depth+1, out)
	}
}

func panelBlockIDOf(n *navigationdto.NavigationNode) string {
	if n == nil || n.PanelBlockID == nil {
		return ""
	}
	return strings.TrimSpace(*n.PanelBlockID)
}

func normalizeNavKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case navKindHeader, navKindHeaderMobile, navKindFooter, navKindFooterMobile:
		return strings.TrimSpace(kind)
	}
	return navKindHeader
}

func (h *navigationPageHandle) NavigationUpdate(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))

	echo := navEditEcho{
		ID: id, ProjectID: projectID, Kind: strings.TrimSpace(c.PostForm("kind")),
		Title: c.PostForm("title"), Path: c.PostForm("path"),
		Target: c.PostForm("target"), UpdatedAt: c.PostForm("expectedUpdatedAt"),
		PanelBlockID: c.PostForm("panelBlockId"), PanelWidth: c.PostForm("panelWidth"),
	}
	if id == "" {
		if navEditHTMX(c) {
			h.renderNavigationEdit(c, echo, navInvalidParamMsg)
		} else {
			navJumpListMenu(c, false, navInvalidParamMsg)
		}
		return
	}
	if strings.TrimSpace(echo.UpdatedAt) == "" {
		c.String(http.StatusBadRequest, "%s", response.MsgText(c, navInvalidParamMsg))
		return
	}
	if !h.navEditOwnedRow(c, echo) {
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

	if v := strings.TrimSpace(c.PostForm("expectedUpdatedAt")); v != "" {
		req.ExpectedUpdatedAt = &v
	}
	if _, err := h.navigations.Update(c.Request.Context(), req); err != nil {

		logger.Scene("page").With("id", id).Error(err, "更新导航项失败")
		h.navEditFailure(c, err, echo)
		return
	}
	navJumpList(c, true, navDoneNotice)
}

var errPanelUnavailable = errors.New("面板块能力未装配")

type BlockPanelPort interface {
	List(ctx context.Context, req *blockcontract.ListReq) ([]blockcontract.BlockResp, error)
	Create(ctx context.Context, req *blockcontract.CreateReq) (*blockcontract.BlockResp, error)
}

func (h *navigationPageHandle) SetBlockPanelPort(p BlockPanelPort) {
	if h == nil {
		return
	}
	h.blocks = p
}

func (h *navigationPageHandle) PanelSet(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := strings.TrimSpace(c.PostForm("id"))

	echo := navEditEcho{
		ID: id, ProjectID: projectID, Kind: strings.TrimSpace(c.PostForm("kind")),
		PanelBlockID: c.PostForm("panelBlockId"), PanelWidth: c.PostForm("panelWidth"),
		UpdatedAt: c.PostForm("expectedUpdatedAt"),
		Title:     c.PostForm("title"), Path: c.PostForm("path"), Target: c.PostForm("target"),
	}
	if id == "" {
		if navEditHTMX(c) {
			h.renderNavigationEdit(c, echo, navInvalidParamMsg)
		} else {
			navJumpListMenu(c, false, navInvalidParamMsg)
		}
		return
	}
	if strings.TrimSpace(echo.UpdatedAt) == "" {
		c.String(http.StatusBadRequest, "%s", response.MsgText(c, navInvalidParamMsg))
		return
	}
	if !h.navEditOwnedRow(c, echo) {
		return
	}

	blockID := strings.TrimSpace(c.PostForm("panelBlockId"))
	req := &navigationdto.UpdateReq{ID: id, PanelBlockID: &blockID}
	if w := strings.TrimSpace(c.PostForm("panelWidth")); w != "" {
		req.PanelWidth = &w
	}

	if v := strings.TrimSpace(c.PostForm("expectedUpdatedAt")); v != "" {
		req.ExpectedUpdatedAt = &v
	}
	if _, err := h.navigations.Update(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("id", id).Error(err, "设置菜单悬浮面板失败")
		h.navEditFailure(c, err, echo)
		return
	}
	navJumpListMenu(c, true, navDoneNotice)
}

func (h *navigationPageHandle) PanelCreate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	title := strings.TrimSpace(c.PostForm("title"))

	menuID := strings.TrimSpace(c.PostForm("id"))
	if !h.navEditOwnedRow(c, navEditEcho{ID: menuID, ProjectID: projectID, Kind: strings.TrimSpace(c.PostForm("kind"))}) {
		return
	}
	if h.blocks == nil {
		logger.Scene("page").With("project", projectID).Error(errPanelUnavailable, "新建面板块失败：块能力未装配")
		navJumpListMenu(c, false, adminui.InternalMsg())
		return
	}
	name := panelBlockName(navTr(c), kind, title)
	created, err := h.blocks.Create(c.Request.Context(), &blockcontract.CreateReq{
		ProjectID: projectID, Name: name, Kind: "block",
	})
	if err != nil {
		logger.Scene("page").With("project_id", projectID).Error(err, "新建面板块失败")
		navJumpListMenu(c, false, navigationErrMsg(c, err))
		return
	}

	target := "/workbench?block=" + url.QueryEscape(created.ID)
	if back := navListBackMenu(c); back != "" {
		target += "&returnUrl=" + url.QueryEscape(back)
	}

	navJump(c, true, navDoneNotice, target, navText{})
}

func panelBlockName(tr func(key, fallback string) string, kind, title string) string {
	pos := tr(navigationenums.PanelBlockNameMenu, "菜单")
	switch kind {
	case "header":
		pos = tr(navigationenums.PanelBlockNameHeader, "页眉菜单")
	case "header_mobile":
		pos = tr(navigationenums.PanelBlockNameHeaderMobile, "页眉移动菜单")
	case "footer":
		pos = tr(navigationenums.PanelBlockNameFooter, "页脚菜单")
	case "footer_mobile":
		pos = tr(navigationenums.PanelBlockNameFooterMobile, "页脚移动菜单")
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = tr(navigationenums.PanelBlockNamePanel, "面板")
	}
	return pos + "·" + t
}

type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

type navigationTranslationRow struct {
	Context    string
	NodeID     string
	Kind       string
	Path       string
	Source     string
	SourceHash string
	Target     string
	Translated bool
}

type navigationTranslationGroup struct {
	Kind  string
	Title string
	Rows  []navigationTranslationRow

	LoadFailed bool

	LoadError navigationLoadError
}

type navigationTranslationHandle struct {
	navigations navigationcontract.NavigationService
	projects    projectcontract.ProjectService
	writer      *i18n.ContentWriter
}

type navigationTranslationSourceOpen interface {
	OpenSourceIndex(ctx context.Context, projectID string) (navigationcontract.SourceIndex, error)
}

func (h *navigationTranslationHandle) sourceIndex(ctx context.Context, projectID string) (navigationcontract.SourceIndex, error) {
	if h == nil || h.navigations == nil {
		return nil, nil
	}
	opener, ok := h.navigations.(navigationTranslationSourceOpen)
	if !ok {
		return nil, nil
	}
	return opener.OpenSourceIndex(ctx, projectID)
}

func indexFailureFact(err error) navigationLoadError {
	return navigationLoadErrorOf(err)
}

func NewNavigationTranslationHandle(navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService) *navigationTranslationHandle {
	return &navigationTranslationHandle{navigations: navigations, projects: projects}
}

func (h *navigationTranslationHandle) SetContentWriter(w *i18n.ContentWriter) { h.writer = w }

func navigationTranslationContext() string {
	return i18n.ContentContext("navigation", "label")
}

func (h *navigationTranslationHandle) NavigationTranslations(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project"))
	lang := strings.TrimSpace(c.Query("lang"))
	data := h.build(c.Request.Context(), projectID, lang, navTr(c))
	data.filter(strings.TrimSpace(c.Query("keyword")))

	data.LoadDegraded = data.anyGroupFailed()
	pageview.Render(c, data)
}

type navigationTranslationsData struct {
	Title        string
	Menu         string
	ProjectID    string
	Keyword      string
	NoProject    bool
	HasData      bool
	VisibleCount int
	Lang         string
	Langs        []translationLangOption
	Groups       []navigationTranslationGroup

	LoadDegraded bool
	RowCount     int
	Done         int
	Saved        bool
	SavedNote    string
	Errors       []string

	SuccessURL string

	kindsByHash map[string][]string
}

func (d *navigationTranslationsData) filter(keyword string) {
	d.Keyword = keyword
	d.VisibleCount = d.RowCount
	if keyword == "" {
		return
	}
	needle := strings.ToLower(keyword)
	for i := range d.Groups {
		rows := d.Groups[i].Rows
		selected := make([]navigationTranslationRow, 0, len(rows))
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Source+" "+row.Path), needle) {
				selected = append(selected, row)
			}
		}
		d.Groups[i].Rows = selected
		d.VisibleCount -= len(rows) - len(selected)
	}

}

func (d *navigationTranslationsData) anyGroupFailed() bool {
	if d == nil {
		return false
	}
	for i := range d.Groups {
		if d.Groups[i].LoadFailed {
			return true
		}
	}
	return false
}

func (d *navigationTranslationsData) noteHashKind(hash, kind string) {
	if d == nil || hash == "" || kind == "" {
		return
	}
	if d.kindsByHash == nil {
		d.kindsByHash = map[string][]string{}
	}
	for _, k := range d.kindsByHash[hash] {
		if k == kind {
			return
		}
	}
	d.kindsByHash[hash] = append(d.kindsByHash[hash], kind)
}

func (d *navigationTranslationsData) kindsForHashes(hashes map[string]bool, fallback []string) []string {
	if d == nil || len(hashes) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for h := range hashes {
		for _, kind := range d.kindsByHash[h] {
			if strings.TrimSpace(kind) == "" || seen[kind] {
				continue
			}
			seen[kind] = true
			out = append(out, kind)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func (d *navigationTranslationsData) sourceOf(hash string) (navigationTranslationRow, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.SourceHash == hash {
				return r, true
			}
		}
	}
	return navigationTranslationRow{}, false
}

func scopeProjectID(data *navigationTranslationsData, formProjectID string) string {
	if data != nil && strings.TrimSpace(data.ProjectID) != "" {
		return strings.TrimSpace(data.ProjectID)
	}
	return strings.TrimSpace(formProjectID)
}

func navigationTranslationLocation(projectID, lang string) string {
	return navigationTranslationFilteredLocation(projectID, lang, "")
}

func navigationTranslationFilteredLocation(projectID, lang, keyword string) string {
	q := url.Values{"lang": {lang}}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	return "/admin/navigations/translations?" + q.Encode()
}

func (h *navigationTranslationHandle) build(ctx context.Context, projectID, lang string,
	tr func(key, fallback string) string) *navigationTranslationsData {
	data := &navigationTranslationsData{

		Title: navigationenums.TranslationHeading, Menu: "navigation-translations",
		ProjectID: projectID, Lang: lang,
		Groups:    []navigationTranslationGroup{},
		NoProject: projectID == "" && h.projects == nil,
	}
	for _, code := range i18n.AvailableLangs() {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}
	if h.projects != nil {
		projects, perr := h.projects.List(ctx)
		if perr != nil {
			logger.Scene("navigation").Error(perr, "读取导航译文工程列表失败")
			data.Errors = []string{tr(navigationenums.ErrProjectListFailed, "读取工程列表失败，请稍后重试")}
			return data
		}
		data.NoProject = len(projects) == 0
		if projectID == "" && len(projects) > 0 {
			projectID = projects[0].ID
			data.ProjectID = projectID
		}
	}
	if lang == "" || h.navigations == nil {
		return data
	}
	if projectID == "" {
		return data
	}
	index, ierr := h.sourceIndex(ctx, projectID)
	if ierr != nil {

		logger.Scene("navigation").With("project", projectID).Error(ierr, "读取导航来源索引失败")
		fact := indexFailureFact(ierr)
		data.Errors = []string{tr(fact.Key, fact.Fallback)}
		return data
	}
	if index == nil {

		logger.Scene("navigation").With("project", projectID).
			Error(errors.New("导航来源索引未装配"), "导航译文工作台：来源索引端口缺失")
		fact := navigationLoadErrorOf(errors.New("导航来源索引未装配"))
		data.Errors = []string{tr(fact.Key, fact.Fallback)}
		return data
	}

	failed := map[string]navigationLoadError{}
	for _, f := range index.KindErrors() {
		if f.Kind == "" {
			continue
		}
		failed[f.Kind] = navigationLoadErrorOf(f.Err)
	}

	seen := map[string]bool{}
	hashes := make([]string, 0, 8)
	type rowRef struct {
		groupIdx int
		rowIdx   int
		hash     string
	}
	var refs []rowRef

	groupOf := map[string]int{}
	for _, kind := range navigationTranslationKinds() {

		groupOf[kind] = len(data.Groups)
		fact, ok := failed[kind]
		data.Groups = append(data.Groups, navigationTranslationGroup{
			Kind: kind, Title: navKindTitleKey(kind), Rows: []navigationTranslationRow{},
			LoadFailed: ok, LoadError: fact,
		})
	}
	for _, entry := range index.Entries() {
		if seen[entry.Hash] {
			continue
		}

		if !i18n.ShouldTranslateContent(entry.Source) {
			continue
		}
		seen[entry.Hash] = true
		hashes = append(hashes, entry.Hash)
		home := translationRowKind(entry.Kinds)
		gi, ok := groupOf[home]
		if !ok {

			continue
		}
		data.noteHashKind(entry.Hash, home)
		for _, k := range entry.Kinds {
			if k == home {
				continue
			}
			data.noteHashKind(entry.Hash, k)
		}
		refs = append(refs, rowRef{groupIdx: gi, rowIdx: len(data.Groups[gi].Rows), hash: entry.Hash})
		data.Groups[gi].Rows = append(data.Groups[gi].Rows, navigationTranslationRow{
			Context: entry.Context, Kind: home,
			Path: indexEntryPath(index, entry), Source: entry.Source, SourceHash: entry.Hash,
		})
	}
	for i := range data.Groups {
		data.RowCount += len(data.Groups[i].Rows)
	}
	data.HasData = data.RowCount > 0
	data.VisibleCount = data.RowCount
	if h.writer != nil && len(hashes) > 0 {

		targets, lerr := h.writer.LoadTargetsForProject(ctx, projectID, lang, hashes)
		if lerr != nil {
			logger.Scene("navigation").With("lang", lang).Error(lerr, "读取现有导航译文失败")
		} else {
			for _, r := range refs {
				if t, ok := targets[i18n.ContentIndexKey(r.hash, navigationTranslationContext())]; ok && t != "" {
					data.Groups[r.groupIdx].Rows[r.rowIdx].Target = t
					data.Groups[r.groupIdx].Rows[r.rowIdx].Translated = true
					data.Done++
				}
			}
		}
	}

	data.SuccessURL = navigationTranslationFilteredLocation(data.ProjectID, data.Lang, data.Keyword)
	return data
}

func navKindTitleKey(kind string) string {
	switch kind {
	case navKindHeaderMobile:
		return navigationenums.KindHeaderMobile
	case navKindFooter:
		return navigationenums.KindFooter
	case navKindFooterMobile:
		return navigationenums.KindFooterMobile
	default:
		return navigationenums.KindHeader
	}
}

func navigationTranslationKinds() []string {
	return []string{navKindHeader, navKindHeaderMobile, navKindFooter, navKindFooterMobile}
}

func translationRowKind(kinds []string) string {
	rank := map[string]int{}
	for i, k := range navigationTranslationKinds() {
		rank[k] = i
	}
	best, bestRank := "", len(rank)
	for _, k := range kinds {
		r, ok := rank[k]
		if !ok {
			continue
		}
		if r < bestRank {
			best, bestRank = k, r
		}
	}
	return best
}

func indexEntryPath(index navigationcontract.SourceIndex, entry navigationcontract.TranslationSourceEntry) string {
	if nodes, ok := index.(navigationcontract.SourceIndexNodes); ok {
		for _, n := range nodes.Nodes(entry.Hash) {
			if p := strings.TrimSpace(n.Path); p != "" {
				return p
			}
		}
		return ""
	}
	return ""
}

type navigationLoadError struct {
	Key      string
	Fallback string
}

func navigationLoadErrorOf(err error) navigationLoadError {
	if fact, ok := facing.FactOf(err); ok {
		return navigationLoadError{Key: fact.Key, Fallback: fact.Fallback}
	}
	return navigationLoadError{Key: navigationenums.InternalKey, Fallback: "操作失败，请稍后重试（细节只进日志）"}
}

const navigationListPath = "/admin/navigations"

func navListBack(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), navigationListPath, "/admin", "navigation", "project", "kind")
}

func navListBackMenu(c *gin.Context) string {
	return backpath.FromRequest(c.Request.URL.Query(), navigationListPath, "/admin", "navigation", "project", "kind", "menu")
}

var (
	navListBackTitle = navText{Key: navigationenums.Heading, Fallback: "导航菜单"}

	navDoneNotice = navText{Key: navigationenums.ActionDone, Fallback: "操作已完成"}
)

func navJump(c *gin.Context, ok bool, msg navText, _ string, _ navText) {
	if ok {
		pageview.SuccessWith(c, pageview.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	pageview.FailMessage(c, navigationErrScene, msg.Key, msg.Fallback, msg.Args...)
}

func navJumpList(c *gin.Context, ok bool, msg navText) {
	navJump(c, ok, msg, navListBack(c), navListBackTitle)
}

func navJumpListMenu(c *gin.Context, ok bool, msg navText) {
	navJump(c, ok, msg, navListBackMenu(c), navListBackTitle)
}

type navigationEditFormView struct {
	CSRFToken          string
	NavEditEcho        any
	NavEditError       string
	NavEditPanelAvail  bool
	NavEditPanelBlocks any
}
