package commenthttp

// 一页管一件事：**审核队列**（列表 + 筛选 + 批量通过 / 驳回）。
//
// 为什么是一页而不是两页（待审 / 已审分开）：审核是一个**连续动作** ——
// 运营打开页面、逐条看、批量处理、再看下一条。分成两页会让「看一眼已驳回的某条
// 是不是手滑」变成跨页跳转；而状态筛选（`?status=`）已经把「只看待审」这件事
// 变成一次点击。
//
// 页面形态照本仓既有后台页：GET 渲染整页、POST 走表单 302 回列表（?err= / ?done= 回带），
// 数据由 templateMap 组装（模板只渲染、不查询）。错误一律经 comment_err.go 的三件套，
// 后台页 handler **不直出内部错误**（门禁 scripts/check-no-internal-error-leak.sh）。
//
// 工程是**必选上下文**：评论按工程隔离（RLS 作用域也要求它），所以页面顶部有工程下拉，
// 且「一个工程都没有」是一种明确的空态（引导去建工程），而不是一个空白列表。

// 与 comment_page_handle.go 分开：那个文件是「页面怎么组装」，本文件是「一行、一个选项长什么样」。
// 行视图在这里定型的好处是「列表列」与「筛选栏」不会各造一份形状 —— 同一个状态在
// 两处显示成不同文案，是这类页面最常见的静默不一致。

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/comment/contract"
	"go_wp/internal/module/comment/dto"
	"go_wp/internal/module/comment/enums"
	"go_wp/internal/module/project/dto"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// commentProjectLister 后台页需要的工程读取能力（**收窄到一条方法**）。
//
// 为什么不直接用 projectcontract.ProjectService：那是一个大接口（工程 CRUD / 主题 /
// 语言 / 槽位都在里面），页面只需要「列出全部工程」这一条 —— 收窄之后，
// 页面层拿不到「改工程」「删主题」这类写能力（越权防护靠接口形状），
// 测试也不必为一个假工程实现二十个方法。
type commentProjectLister interface {
	List(ctx context.Context) ([]projectdto.ProjectResp, error)
}

// commentPageHandle 后台审核页处理器。
type commentPageHandle struct {
	svc      commentcontract.CommentService
	projects commentProjectLister
}

// NewCommentPageHandle 构造。
func NewCommentPageHandle(svc commentcontract.CommentService,
	projects commentProjectLister) *commentPageHandle {
	return &commentPageHandle{svc: svc, projects: projects}
}

// commentsPageData 审核页的模板数据（正常渲染与降级渲染共用一份拼装）。
//
// 单独一个结构体的理由：降级分支若另抄一份 gin.H，两处的键集必然分叉，
// 而模板缺 key 的后果是 renderError → HTTP 500（Jet 类型不符会让整页渲染中断）。
type commentsPageData struct {
	Title           string
	Menu            string
	Projects        []projectdto.ProjectResp
	SelectedProject string
	Rows            []commentRow
	StatusOptions   []gin.H
	EntityOptions   []gin.H

	FilterStatus     string
	FilterEntityType string
	FilterKeyword    string
	FilterValues     commentFilterValues

	Total int64
	Page  int
	// NoProject 一个站点工程都没有：评论按工程隔离，这一页无从下手（明确的空态）。
	NoProject bool
	// ListQuery 列表上下文（工程 / 筛选 / 页码）的查询串，拼进写动作表单的 action。
	//
	// 服务端自己拼（shell.WithParams 同款口径）：页面不需要把上下文塞进隐藏域，
	// 回跳时由 shell.BackPath 按白名单读回来。
	ListQuery string
	// LoadErr 这一次没读出来的原因（空串 = 正常）。
	//
	// 写动作的结论不在这里：它由提示页在响应体里渲染（shell.RenderJump），
	// 不再经 ?err= / ?done= 回带 —— 查询参数不是可信边界，那套读侧白名单随之消失。
	LoadErr string
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
func (d *commentsPageData) templateMap(c *gin.Context) gin.H {
	keys := gin.H{
		"title":           d.Title,
		"menu":            d.Menu,
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Rows":            d.Rows,
		"HasRows":         len(d.Rows) > 0,
		"StatusOptions":   d.StatusOptions,
		"EntityOptions":   d.EntityOptions,
		"FilterStatus":    d.FilterStatus,
		"FilterEntity":    d.FilterEntityType,
		"FilterKeyword":   d.FilterKeyword,
		"Total":           d.Total,
		"Page":            d.Page,
		"NoProject":       d.NoProject,
		"ListQuery":       d.ListQuery,
		"LoadErr":         d.LoadErr,
	}
	// 分页条的两个键：单页 / 空数据时为空 map，模板的 {{if}} 自然跳过。
	for k, v := range paginationKeys(c, d.Total, d.Page, d.FilterValues) {
		keys[k] = v
	}
	return keys
}

// render 审核页的唯一渲染出口（正常与降级两条路都从这里出）。
func (h *commentPageHandle) render(c *gin.Context, d *commentsPageData) {
	c.HTML(http.StatusOK, "admin/comment/comments.html", shell.Prepare(c, d.templateMap(c)))
}

// CommentsPage 审核队列页（GET /admin/comments）。
func (h *commentPageHandle) CommentsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)

	title := tr(commentenums.PageTitle.Key, commentenums.PageTitle.Fallback)
	data := &commentsPageData{
		Title:            title,
		Menu:             "comment",
		StatusOptions:    commentStatusOptions(c, ""),
		FilterStatus:     strings.TrimSpace(c.Query("status")),
		FilterEntityType: strings.TrimSpace(c.Query("entityType")),
		FilterKeyword:    strings.TrimSpace(c.Query("keyword")),
		Page:             queryInt(c.Query("page")),
	}

	projects, loadErr := h.listProjects(ctx)
	if loadErr != nil {
		data.Rows = []commentRow{}
		data.LoadErr = commentErrPageText(c, loadErr)
		h.render(c, data)
		return
	}
	data.Projects = projects
	data.SelectedProject = pickCommentProject(c.Query("project"), projects)
	data.ListQuery = commentListQuery(data.SelectedProject, data.FilterStatus, data.FilterEntityType, data.FilterKeyword, data.Page)
	if data.SelectedProject == "" {
		data.NoProject = true
		data.Rows = []commentRow{}
		data.StatusOptions = commentStatusOptions(c, data.FilterStatus)
		data.EntityOptions = commentEntityOptions(c, h.svc, data.FilterEntityType)
		h.render(c, data)
		return
	}

	data.FilterValues = commentFilterValues{
		Project:    data.SelectedProject,
		Status:     data.FilterStatus,
		EntityType: data.FilterEntityType,
		Keyword:    data.FilterKeyword,
	}
	data.StatusOptions = commentStatusOptions(c, data.FilterStatus)
	data.EntityOptions = commentEntityOptions(c, h.svc, data.FilterEntityType)

	res, listErr := h.svc.AdminList(ctx, &commentdto.AdminListReq{
		ProjectID:  data.SelectedProject,
		Status:     data.FilterStatus,
		EntityType: data.FilterEntityType,
		Keyword:    data.FilterKeyword,
		Page:       data.Page,
		PageSize:   commentsPageSize,
	})
	if listErr != nil {
		if data.LoadErr == "" {
			data.LoadErr = commentErrPageText(c, listErr)
		}
		data.Rows = []commentRow{}
		h.render(c, data)
		return
	}
	labels := commentEntityLabelMap(c, h.svc)
	rows := make([]commentRow, 0, len(res.Items))
	for _, item := range res.Items {
		rows = append(rows, commentRowOf(tr, labels, item))
	}
	data.Rows = rows
	data.Total = res.Total
	data.Page = res.Page
	h.render(c, data)
}

// Review 批量通过 / 驳回（POST /admin/comments/review）。
//
// 权限不在本函数里判：页面写动作经 builtin.CasbinMiddlewareForPath("/api/comment/review")
// 复用接口的权限点（真源在 comment_router.go 的接口注册处）。
func (h *commentPageHandle) Review(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	status := strings.TrimSpace(c.PostForm("status"))
	// 回跳上下文**随表单 action 的 query** 一起提交，服务端按白名单读回来 ——
	// 不再用 filterStatus / filterEntityType / filterKeyword 三个隐藏域，
	// 也不再由 Go 拼 ?err= / ?done= 的回跳 URL（结论走响应体，见 shell.RenderJump）。
	back := shell.BackPath(c, commentsPath, "project", "status", "entityType", "keyword", "page")
	backText := shell.TranslateFor(c)(commentenums.PageTitle.Key, commentenums.PageTitle.Fallback)

	ids, err := commentIDsFromForm(c)
	if err != nil {
		// 超限：整批拒绝并把可展示的原因渲染成提示页（文案来自 shell 的白名单出口）。
		commentReviewFail(c, shell.BulkIDsFacingText(c, err), back, backText)
		return
	}
	if len(ids) == 0 {
		commentReviewFail(c,
			shell.TranslateFor(c)(commentenums.LabelKeyErrNothingSelected, commentenums.LabelErrNothingSelected),
			back, backText)
		return
	}

	res, rerr := h.svc.Review(c.Request.Context(), &commentdto.ReviewReq{
		ProjectID:  projectID,
		IDs:        ids,
		Status:     status,
		ReviewerID: shell.CurrentUserID(c),
	})
	if rerr != nil {
		commentReviewFail(c, commentErrPageText(c, rerr), back, backText)
		return
	}
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      commentReviewNotice(c, res),
		Back:     back,
		BackText: backText,
		Seconds:  1,
	})
}

// commentReviewFail 审核失败的出口：整页提示（不自动跳转，用户要看清原因）。
//
// 文案必须**已过本模块的白名单**（commentErrPageText / shell 的受控出口）——
// 换个页面呈现不等于可以把 err.Error() 铺在页面上。
func commentReviewFail(c *gin.Context, msg, back, backText string) {
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// commentReviewNotice 审核完成回执（带条数的成品文案）。
//
// Sprintf 的模板来自词条（缺词条时用中文兜底）：**模板与条数都不可信** ——
// 词条若被改坏（例如混进 %!d 这类协议外占位符），Sprintf 会输出一段垃圾，
// 所以模板必须先过 i18n 的占位符检查（同 shell.BulkIDsFacingText 的判据）。
func commentReviewNotice(c *gin.Context, res *commentdto.ReviewResp) string {
	if res == nil {
		return ""
	}
	pair := commentenums.DoneTemplate(res.Status == commentenums.StatusApproved)
	tpl := shell.TranslateFor(c)(pair.Key, pair.Fallback)
	if !i18n.HasStringPlaceholdersOnly(tpl) {
		tpl = pair.Fallback
	}
	return fmt.Sprintf(tpl, res.Affected)
}

// listProjects 列出站点工程（失败时返回错误，由调用方降级渲染）。
func (h *commentPageHandle) listProjects(ctx context.Context) ([]projectdto.ProjectResp, error) {
	if h.projects == nil {
		return nil, nil
	}
	return h.projects.List(ctx)
}

// 页面路径与分页参数（分页上限与 service 的 MaxPageSize 同口径：超过即被 service 压下）。
const (
	commentsPath     = "/admin/comments"
	commentsPageSize = 20
)

// commentRow 审核列表的一行（模板直接渲染，不在模板里做换算或判断）。
type commentRow struct {
	ID          int64
	Body        string
	EntityType  string
	EntityLabel string
	EntityID    string
	UserID      uint64
	Status      string
	StatusLabel string
	// StatusClass 状态徽章的样式类（由服务端算好：Jet 里做这种映射的代价是出错时整页 500）。
	StatusClass string
	CreateTime  string
	// Reviewed 审核信息（「已通过 · 管理员 #3」这类成品文案；未审核为空串）。
	Reviewed string
	IsReply  bool
}

// commentRowOf 后台行视图。
//
// tr 与实体展示名映射由调用点传入（不在这里现取）：行视图保持纯函数，
// 「同一行在不同语言下渲染成什么」才可以被单测直接钉住（同 membership 的取舍）。
func commentRowOf(tr func(key, fallback string) string, entityLabels map[string]string,
	item commentdto.AdminItem) commentRow {
	label := entityLabels[item.EntityType]
	if label == "" {
		// 未登记的实体类型原样回显标识：显示一个陌生的英文值，好过把信息藏起来
		// （它还可能是「注册被删掉了」的现场证据）。
		label = item.EntityType
	}
	pair := commentenums.StatusLabel(item.Status)
	row := commentRow{
		ID:          item.ID,
		Body:        item.Body,
		EntityType:  item.EntityType,
		EntityLabel: label,
		EntityID:    item.EntityID,
		UserID:      item.UserID,
		Status:      item.Status,
		StatusLabel: tr(pair.Key, pair.Fallback),
		StatusClass: commentStatusClass(item.Status),
		CreateTime:  item.CreateTime.Time().Format("2006-01-02 15:04"),
		IsReply:     item.IsReply,
	}
	if item.ReviewedAt != nil {
		when := item.ReviewedAt.Time().Format("2006-01-02 15:04")
		if item.ReviewerID != nil {
			// 审核人显示账号 id：这是控制面，运营需要据此追责（谁的判断）。
			row.Reviewed = when + " · #" + strconv.FormatUint(*item.ReviewerID, 10)
		} else {
			row.Reviewed = when
		}
	}
	return row
}

// commentStatusClass 状态 → 徽章样式类（与既有后台页的 badge 词表一致）。
func commentStatusClass(status string) string {
	switch status {
	case commentenums.StatusApproved:
		return "badge-success"
	case commentenums.StatusRejected:
		return "badge-warning"
	case commentenums.StatusSpam:
		return "badge-danger"
	default:
		return "badge"
	}
}

// pickCommentProject 从请求参数与工程列表里挑一个当前工程。
//
// 规则（与 membership 的 pickProject 同一条）：请求里指定的 id 必须在列表里，
// 否则回落到第一个工程 —— 「贴来一个已删除工程的链接」不该变成一次报错，
// 它应当只是把这个页面切回默认工程。
func pickCommentProject(requested string, projects []projectdto.ProjectResp) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		for _, p := range projects {
			if p.ID == requested {
				return requested
			}
		}
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

// commentStatusOptions 状态筛选下拉（Selected 由服务端算好，模板只渲染）。
func commentStatusOptions(c *gin.Context, selected string) []gin.H {
	tr := shell.TranslateFor(c)
	out := make([]gin.H, 0, len(commentenums.Statuses())+1)
	out = append(out, gin.H{
		"Value":    "",
		"Label":    tr(commentenums.LabelKeyFilterAll, commentenums.LabelFilterAll),
		"Selected": selected == "",
	})
	for _, status := range commentenums.Statuses() {
		pair := commentenums.StatusLabel(status)
		out = append(out, gin.H{
			"Value":    status,
			"Label":    tr(pair.Key, pair.Fallback),
			"Selected": status == selected,
		})
	}
	return out
}

// commentEntityOptions 实体类型筛选下拉（取值来自 service 的注册表 —— 拥有者声明的那份）。
func commentEntityOptions(c *gin.Context, svc commentcontract.CommentService, selected string) []gin.H {
	tr := shell.TranslateFor(c)
	out := []gin.H{{
		"Value":    "",
		"Label":    tr(commentenums.LabelKeyFilterAll, commentenums.LabelFilterAll),
		"Selected": selected == "",
	}}
	if svc == nil {
		return out
	}
	for _, et := range svc.EntityTypeLabels(tr) {
		out = append(out, gin.H{
			"Value":    et.Type,
			"Label":    et.Label,
			"Selected": et.Type == selected,
		})
	}
	return out
}

// commentEntityLabelMap 实体类型 → 展示名（行视图用；同样取 service 的注册表）。
func commentEntityLabelMap(c *gin.Context, svc commentcontract.CommentService) map[string]string {
	out := map[string]string{}
	if svc == nil {
		return out
	}
	for _, et := range svc.EntityTypeLabels(shell.TranslateFor(c)) {
		out[et.Type] = et.Label
	}
	return out
}

// commentFilterValues 当前筛选条件（分页链接与回跳地址都要带上它们，
// 否则「翻到第 2 页」会丢掉筛选 —— 那是最容易被当成「筛选没生效」的缺陷）。
type commentFilterValues struct {
	Project    string
	Status     string
	EntityType string
	Keyword    string
}

// paginationKeys 分页条的两个模板键（单页 / 空数据时返回空 map，模板自然不渲染）。
func paginationKeys(c *gin.Context, total int64, page int, f commentFilterValues) map[string]any {
	q := url.Values{}
	q.Set("project", f.Project)
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.EntityType != "" {
		q.Set("entityType", f.EntityType)
	}
	if f.Keyword != "" {
		q.Set("keyword", f.Keyword)
	}
	base := commentsPath + "?" + q.Encode()
	pg := shell.BuildPagination(total, page, commentsPageSize, base, shell.TranslateFor(c))
	if pg == nil {
		return map[string]any{}
	}
	return pg.TemplateKeys()
}

// queryInt 读一个非负整数查询参数（解析失败返回 0，由调用方决定默认值）。
//
// 为什么不直接用 strconv.Atoi 并在失败时报错：页码 / 条数是**展示参数**，
// 手改 URL 写坏了应当回落到默认值，而不是给运营一个「参数不合法」的错误页。
func queryInt(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// commentIDsFromForm 读批量审核的 id 列表（经 shell.BulkIDs 去空白 / 去重 / 限量）。
//
// 返回值第二个是「超限」这类可展示的拒绝原因（命中 shell 的白名单），
// 由调用方回带进 ?err= —— 整批拒绝而不是截断执行（见 shell.BulkIDs 的说明）。
func commentIDsFromForm(c *gin.Context) ([]int64, error) {
	raw, err := shell.BulkIDs(c)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		n, perr := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if perr != nil || n <= 0 {
			// 非数字 id 不是「可展示的业务错误」（没有一条给运营看的话说得通），
			// 而是有人在手工构造表单 —— 跳过它，但**留痕**（静默跳过会让
			// 「选了 5 条只处理了 3 条」变得无法解释）。
			logger.Scene(commentErrScene).
				With("user_id", shell.CurrentUserID(c)).
				With("path", c.Request.URL.Path).
				Warn("批量审核的 id 不是正整数，已跳过该条")
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// commentListQuery 列表上下文的查询串（拼进写动作表单的 action）。
//
// 与 order 的 couponListQuery 同形：空值丢弃、编码走 url.Values（键有序、产物稳定）。
// 只带**筛选上下文**，不带任何结论文案 —— 结论走响应体（见 shell.RenderJump）。
func commentListQuery(projectID, status, entityType, keyword string, page int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("status", status)
	set("entityType", entityType)
	set("keyword", keyword)
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	return q.Encode()
}
