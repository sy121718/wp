// comment_page_util.go — 后台审核页的行视图、下拉选项与分页键（BIZ-5）。
//
// 与 comment_page_handle.go 分开：那个文件是「页面怎么组装」，本文件是「一行、一个选项长什么样」。
// 行视图在这里定型的好处是「列表列」与「筛选栏」不会各造一份形状 —— 同一个状态在
// 两处显示成不同文案，是这类页面最常见的静默不一致。
package commenthttp

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

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
