package commenthttp

// 一页管一件事：**审核队列**（列表 + 筛选 + 批量通过 / 驳回）。
//
// 为什么是一页而不是两页（待审 / 已审分开）：审核是一个**连续动作** ——
// 运营打开页面、逐条看、批量处理、再看下一条。分成两页会让「看一眼已驳回的某条
// 是不是手滑」变成跨页跳转；而状态筛选（`?status=`）已经把「只看待审」这件事
// 变成一次点击。
//
// 页面 GET 渲染整页，审核写操作由浏览器直调 /api/comment/review；
// 数据由 templateMap 组装（模板只渲染、不查询）。错误一律经 comment_err.go 的三件套，
// 后台页 handler **不直出内部错误**（门禁 scripts/check-no-internal-error-leak.sh）。
//
// 工程是**必选上下文**：评论按工程隔离（RLS 作用域也要求它），所以页面顶部有工程下拉，
// 且「一个工程都没有」是一种明确的空态（引导去建工程），而不是一个空白列表。

// 页面装配与行视图同处一份实现，列表与筛选共享状态文案。

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
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
	// LoadErr 这一次没读出来的原因（空串 = 正常）。
	//
	// 审核结论由 JSON API 返回成品文案，此处仅承载读取失败提示。
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
		"LoadErr":         d.LoadErr,
	}
	// 分页**事实**（pagination.Page，值语义）：零值＝没有分页，模板据 HasPagination()
	// 决定渲不渲染分页条；文案与链接由 admin/partials/pagination.html 渲染。
	keys["Pagination"] = paginationFacts(d.Total, d.Page, d.FilterValues)
	return keys
}

// render 审核页的唯一渲染出口（正常与降级两条路都从这里出）。
func (h *commentPageHandle) render(c *gin.Context, d *commentsPageData) {
	c.HTML(http.StatusOK, "admin/comment/comments.html", adminui.Prepare(c, d.templateMap(c)))
}

// CommentsPage 审核队列页（GET /admin/comments）。
func (h *commentPageHandle) CommentsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := i18n.TranslateFunc(response.RequestLanguage(c))

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
		data.LoadErr = response.MessageOf(c, "comment", loadErr)
		h.render(c, data)
		return
	}
	data.Projects = projects
	data.SelectedProject = pickCommentProject(c.Query("project"), projects)
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
			data.LoadErr = response.MessageOf(c, "comment", listErr)
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
	tr := i18n.TranslateFunc(response.RequestLanguage(c))
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
	tr := i18n.TranslateFunc(response.RequestLanguage(c))
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
	for _, et := range svc.EntityTypeLabels(i18n.TranslateFunc(response.RequestLanguage(c))) {
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

// paginationFacts 分页事实（值语义：零值＝没有分页，模板据此不渲染分页条）。
//
// 基址带上审核页的全部筛选条件（翻页不丢条件），页码参数与 limit 沿用默认
// （PageParam="page"、KeepLimit=true）—— 与迁移前的链接字节一致。
func paginationFacts(total int64, page int, f commentFilterValues) pagination.Page {
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
	pg := pagination.Facts(total, page, commentsPageSize)
	pg.Base = commentsPath + "?" + q.Encode()
	return pg
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
