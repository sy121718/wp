// comment_page_handle.go — 评论审核后台页（BIZ-5）。
//
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
package commenthttp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
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
	// LoadFailed 工程列表或评论列表没读出来（降级渲染；与「真的一条评论都没有」必须分辨）。
	LoadFailed bool
	Err        string
	Done       string
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
		"LoadFailed":      d.LoadFailed,
		"Err":             d.Err,
		"Done":            d.Done,
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
	pageErr := commentPageErr(c)

	title := tr(commentenums.PageTitle.Key, commentenums.PageTitle.Fallback)
	data := &commentsPageData{
		Title:            title,
		Menu:             "comment",
		StatusOptions:    commentStatusOptions(c, ""),
		FilterStatus:     strings.TrimSpace(c.Query("status")),
		FilterEntityType: strings.TrimSpace(c.Query("entityType")),
		FilterKeyword:    strings.TrimSpace(c.Query("keyword")),
		Page:             queryInt(c.Query("page")),
		Err:              pageErr,
		Done:             commentPageDone(c),
	}

	projects, loadErr := h.listProjects(ctx)
	if loadErr != nil {
		data.LoadFailed = true
		data.Rows = []commentRow{}
		// 装载失败压过 ?err=：它是这次请求真实发生的事，URL 里那条是上一次写失败的旧提示。
		data.Err = commentErrPageText(c, loadErr)
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
		if data.Err == "" {
			data.Err = commentErrPageText(c, listErr)
		}
		data.LoadFailed = true
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
	// 回跳时保留筛选条件：审核完回到同一个视图，而不是被弹回「全部状态」。
	filter := commentFilterValues{
		Project:    projectID,
		Status:     strings.TrimSpace(c.PostForm("filterStatus")),
		EntityType: strings.TrimSpace(c.PostForm("filterEntityType")),
		Keyword:    strings.TrimSpace(c.PostForm("filterKeyword")),
	}

	ids, err := commentIDsFromForm(c)
	if err != nil {
		// 超限：整批拒绝并把可展示的原因回带（文案来自 shell 的白名单出口）。
		c.Redirect(http.StatusFound, commentErrURLText(filter, shell.BulkIDsFacingText(c, err)))
		return
	}
	if len(ids) == 0 {
		c.Redirect(http.StatusFound, commentErrURLText(filter,
			shell.TranslateFor(c)(commentenums.LabelKeyErrNothingSelected, commentenums.LabelErrNothingSelected)))
		return
	}

	res, rerr := h.svc.Review(c.Request.Context(), &commentdto.ReviewReq{
		ProjectID:  projectID,
		IDs:        ids,
		Status:     status,
		ReviewerID: shell.CurrentUserID(c),
	})
	if rerr != nil {
		c.Redirect(http.StatusFound, commentErrURL(c, filter, rerr))
		return
	}
	c.Redirect(http.StatusFound, commentOKURL(filter, commentReviewNotice(c, res)))
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
