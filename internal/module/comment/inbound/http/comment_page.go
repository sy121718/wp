package commenthttp

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

type commentProjectLister interface {
	List(ctx context.Context) ([]projectdto.ProjectResp, error)
}

type commentPageHandle struct {
	svc      commentcontract.CommentService
	projects commentProjectLister
}

func NewCommentPageHandle(svc commentcontract.CommentService,
	projects commentProjectLister) *commentPageHandle {
	return &commentPageHandle{svc: svc, projects: projects}
}

type commentsPageData struct {
	Title           string
	Menu            string
	Projects        []projectdto.ProjectResp
	SelectedProject string
	Rows            []commentRow
	StatusOptions   []commentOptionResp
	EntityOptions   []commentOptionResp

	FilterStatus string

	FilterEntity  string
	FilterKeyword string
	FilterValues  commentFilterValues

	Total int64
	Page  int

	NoProject bool

	LoadErr string

	HasRows bool

	Pagination pagination.Page
}

type commentOptionResp struct {
	Value    string
	Label    string
	Selected bool
}

func (h *commentPageHandle) render(c *gin.Context, d *commentsPageData) {
	d.Pagination = paginationFacts(d.Total, d.Page, d.FilterValues)
	pageview.Render(c, d)
}

func (h *commentPageHandle) CommentsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := i18n.TranslateFunc(response.RequestLanguage(c))

	title := tr(commentenums.PageTitle.Key, commentenums.PageTitle.Fallback)
	data := &commentsPageData{
		Title:         title,
		Menu:          "comment",
		StatusOptions: commentStatusOptions(c, ""),
		FilterStatus:  strings.TrimSpace(c.Query("status")),
		FilterEntity:  strings.TrimSpace(c.Query("entityType")),
		FilterKeyword: strings.TrimSpace(c.Query("keyword")),
		Page:          queryInt(c.Query("page")),
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
		data.EntityOptions = commentEntityOptions(c, h.svc, data.FilterEntity)
		h.render(c, data)
		return
	}

	data.FilterValues = commentFilterValues{
		Project:    data.SelectedProject,
		Status:     data.FilterStatus,
		EntityType: data.FilterEntity,
		Keyword:    data.FilterKeyword,
	}
	data.StatusOptions = commentStatusOptions(c, data.FilterStatus)
	data.EntityOptions = commentEntityOptions(c, h.svc, data.FilterEntity)

	res, listErr := h.svc.AdminList(ctx, &commentdto.AdminListReq{
		ProjectID:  data.SelectedProject,
		Status:     data.FilterStatus,
		EntityType: data.FilterEntity,
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

func (h *commentPageHandle) listProjects(ctx context.Context) ([]projectdto.ProjectResp, error) {
	if h.projects == nil {
		return nil, nil
	}
	return h.projects.List(ctx)
}

const (
	commentsPath     = "/admin/comments"
	commentsPageSize = 20
)

type commentRow struct {
	ID          int64
	Body        string
	EntityType  string
	EntityLabel string
	EntityID    string
	UserID      uint64
	Status      string
	StatusLabel string

	StatusClass string
	CreateTime  string

	Reviewed string
	IsReply  bool
}

func commentRowOf(tr func(key, fallback string) string, entityLabels map[string]string,
	item commentdto.AdminItem) commentRow {
	label := entityLabels[item.EntityType]
	if label == "" {

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

			row.Reviewed = when + " · #" + strconv.FormatUint(*item.ReviewerID, 10)
		} else {
			row.Reviewed = when
		}
	}
	return row
}

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

func commentStatusOptions(c *gin.Context, selected string) []commentOptionResp {
	tr := i18n.TranslateFunc(response.RequestLanguage(c))
	out := make([]commentOptionResp, 0, len(commentenums.Statuses())+1)
	out = append(out, commentOptionResp{
		Value:    "",
		Label:    tr(commentenums.LabelKeyFilterAll, commentenums.LabelFilterAll),
		Selected: selected == "",
	})
	for _, status := range commentenums.Statuses() {
		pair := commentenums.StatusLabel(status)
		out = append(out, commentOptionResp{
			Value:    status,
			Label:    tr(pair.Key, pair.Fallback),
			Selected: status == selected,
		})
	}
	return out
}

func commentEntityOptions(c *gin.Context, svc commentcontract.CommentService, selected string) []commentOptionResp {
	tr := i18n.TranslateFunc(response.RequestLanguage(c))
	out := []commentOptionResp{{
		Value:    "",
		Label:    tr(commentenums.LabelKeyFilterAll, commentenums.LabelFilterAll),
		Selected: selected == "",
	}}
	if svc == nil {
		return out
	}
	for _, et := range svc.EntityTypeLabels(tr) {
		out = append(out, commentOptionResp{
			Value:    et.Type,
			Label:    et.Label,
			Selected: et.Type == selected,
		})
	}
	return out
}

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

type commentFilterValues struct {
	Project    string
	Status     string
	EntityType string
	Keyword    string
}

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
	return pagination.Page{
		Total: total, Page: page, Limit: commentsPageSize,
		Base: commentsPath + "?" + q.Encode(),
	}
}

func queryInt(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
