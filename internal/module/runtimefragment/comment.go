package runtimefragment

// comment.go — 评论片段（BIZ-5）。
//
// # 为什么评论必须走运行时片段，而不是烘进静态产物
//
// 产物按**语言**维度编译（internal/pipeline/artifact.go 的 Manifest.Lang），
// 对所有人是**同一份字节**（AGENTS.md 不变量 1）。而评论是实时数据：
// 新评论一提交（并审核通过）就必须可见，而产物不会因为一条评论重新发布。
// 把评论列表烘进产物，结果是「访客看到的是构建那一刻的评论」—— 且没有任何报错。
// 这是 docs/10-todo.md BIZ-5 把「列表走 Runtime Fragment、不进构建期产物」写成硬方向的理由。
//
// # 两个能力
//
//	commentList   —— GET，anonymous：已通过的评论列表 + 提交表单（或登录引导）
//	commentSubmit —— POST，anonymous **策略**（内部判登录）：落 pending，返回可见文案
//
// 为什么 POST 写能力声明 AuthAnonymous 而不是 AuthSession（**重要，别改成 session**）：
//
//   - AuthSession 会让端点未登录时直接回 401，而 **HTMX 默认不替换 401 响应的目标节点**
//     （仓库已记录的判据，见 endpoint.go 与 orders.go / membership.go 的同一段说明）——
//     访客点「发表评论」后页面**毫无变化**，不知道自己需要登录。
//   - 真正的门槛在**数据侧**：没有访客身份就没有 user_id，service 一律拒绝
//     （commentenums.ErrLoginRequired），这里只是把结果渲染成一句人话 + 登录引导。
//   - CSRF 另有一道：本能力要求请求体里的 csrf_token 与访客域的 token 一致
//     （见 renderCommentSubmit），拿不到 token 的请求（跨站伪造表单拿不到 cookie 里的值）
//     一律被拒。缺 token 时渲染可见文案而不是静默。
//
// # 端口与降级
//
//	deps.CommentPort    —— 评论模块的收窄契约（只读列表 + 提交，拿不到审核能力）
//	deps.CommentFacingTexter  —— 把评论模块的业务错误转成可展示文案（片段层拿不到它的 enums 白名单）
//	deps.CommentSourceHasher —— 来源 IP 的带盐哈希函数（**算法与盐都留在评论模块**，装配期注入）
//
// 任一端口未注入或参数缺失一律渲染**可见文案**，绝不 500：片段端点把 error 变成
// 500 + 一句「片段渲染失败」，对访客没有任何信息量（同 membership.go / cart.go 的取舍）。

import (
	"context"
	"crypto/subtle"
	"strconv"
	"strings"

	commentdto "go_wp/internal/module/comment/dto"
	rfenums "go_wp/internal/module/runtimefragment/enums"
	"go_wp/internal/templates"
)

// 两个能力名（同时是模板里的 data-fragment 值）。
const (
	commentFragmentList   = "commentList"
	commentFragmentSubmit = "commentSubmit"
)

// commentMaxBodyLen 评论正文长度上限（表单 maxlength 用）。
//
// 与 comment 模块 commentenums.MaxBodyLen（2000）同值，理由同上：片段层不 import
// 对方的 enums。两处不一致时的表现是「表单允许输入 2500 字，提交后被 service 拒绝」
// —— 用户看到的是「评论太长了」，不是静默失败。
const commentMaxBodyLen = 2000

// commentPageSize 评论列表片段每页条数。
//
// 与 comment 模块的 commentenums.DefaultPageSize（20）同值：片段层不 import 评论模块的
// enums（跨模块只走 contract + 不可变 dto），所以这里独立声明一份，并注释指向真源。
// 两处不一致时的表现只是「片段一页显示的条数与后台列表不同」，不是缺陷。
const commentPageSize = 20

// commentSourceHash 算来源哈希（未注入时给空串）。
func commentSourceHash(ip string) string {
	if deps.CommentSourceHasher == nil {
		return ""
	}
	return deps.CommentSourceHasher(ip)
}

func init() {
	Register(Spec{Type: commentFragmentList, Method: "GET", Auth: AuthAnonymous, Render: renderCommentList})
	Register(Spec{Type: commentFragmentSubmit, Method: "POST", Auth: AuthAnonymous, Render: renderCommentSubmit})
}

// commentFragmentData commentList 的模板数据。
type commentFragmentData struct {
	// Fragment 能力名（模板写成 data-fragment 属性，与 loginPanel / cartSummary 同口径）。
	Fragment string
	// ProjectID / EntityType / EntityID 回填进提交表单（提交端点要用同一组参数）。
	ProjectID  string
	EntityType string
	EntityID   string
	// CSRFToken 访客域 CSRF token（提交表单的隐藏域）。
	CSRFToken string

	// —— 降级形态（页面据此显示对应的人话，四者必须分得开）——
	//
	// Unavailable 端口未注入（装配缺失）：要运维去接线；
	// ConfigMissing 页面没给工程 / 实体（页面作者的配置问题）：要站点编辑去补；
	// NeedLogin 未登录：要访客去登录；
	// Failed 读不出来：可以稍后重试。
	//
	// 四者页面上长得一样的话，装配缺陷会被当成「这站要登录」而被忽略很久
	// （与 membership 的三条降级同一条判据）。
	Unavailable   bool
	ConfigMissing bool
	NeedLogin     bool
	Failed        bool
	// Notice 上面任意一种降级形态的一句话（已按请求语言取词）。
	Notice string

	// CanSubmit 渲染提交表单（已登录 + 拿得到 CSRF token）。
	CanSubmit bool
	// NoToken 已登录但拿不到 CSRF token（多半是片段端点没挂访客身份中间件）：
	// 表单**不渲染**，改为一句可见的说明 —— 渲染一个必然 403 的表单是最糟的形态。
	NoToken bool

	Items []commentItemView
	// HasItems 有评论可渲染（模板不做 len 判断：Jet 里把长度判断写在模板里
	// 出错时整页 500，而这一条完全可以在服务端算清楚）。
	HasItems bool
	Total    int64
	Page     int
	// MaxBodyLen 正文长度上限（表单的 maxlength；与 service 的校验同源）。
	MaxBodyLen int
	// HasMore 还有下一页（模板据此渲染「加载更多」）。
	HasMore bool
	// NextPage 下一页页码（HasMore 时有效）。
	NextPage int

	Labels commentLabels
}

// commentItemView 一条评论的展示视图（已取词 / 已格式化，模板只渲染）。
type commentItemView struct {
	ID        int64
	Body      string
	Author    string
	CreatedAt string
	IsReply   bool
	Replies   []commentItemView
}

// commentSubmitData commentSubmit 的模板数据。
type commentSubmitData struct {
	Fragment string
	// OK 提交成功（先审后发口径下的「成功」= 落 pending）。
	OK bool
	// Notice 结果文案（成功与失败都走这一条：模板只显示一句人话）。
	Notice string
	Labels commentLabels
}

// commentLabels comment 片段的固定文案（由 Go 预翻译后注入 jet）。
//
// 取词一律 `r.tr(rfenums.Xxx, "中文兜底")`：词条缺失时回落中文，
// 而不是在页面上显示裸 key。
type commentLabels struct {
	Title          string
	Empty          string
	FormTitle      string
	Placeholder    string
	Submit         string
	More           string
	AuthorGuest    string
	PendingNotice  string
	Guest          string
	Unavailable    string
	ProjectMissing string
	EntityMissing  string
	EntityInvalid  string
	ListFailed     string
	BodyRequired   string
	BodyTooLong    string
	CSRFExpired    string
	RateLimited    string
	SubmitFailed   string
}

func commentLabelsOf(r *Request) commentLabels {
	return commentLabels{
		Title:          r.tr(rfenums.CommentTitle, "评论"),
		Empty:          r.tr(rfenums.CommentEmpty, "还没有评论，来说点什么吧。"),
		FormTitle:      r.tr(rfenums.CommentFormTitle, "写下你的评论"),
		Placeholder:    r.tr(rfenums.CommentBodyPlaceholder, "说点什么…"),
		Submit:         r.tr(rfenums.CommentSubmit, "发表评论"),
		More:           r.tr(rfenums.CommentMore, "更多评论"),
		AuthorGuest:    r.tr(rfenums.CommentAuthorGuest, "访客"),
		PendingNotice:  r.tr(rfenums.CommentPendingNotice, "评论已提交，待审核通过后显示。"),
		Guest:          r.tr(rfenums.CommentGuest, "登录后可以发表评论。"),
		Unavailable:    r.tr(rfenums.CommentUnavailable, "评论功能暂时不可用。"),
		ProjectMissing: r.tr(rfenums.CommentProjectMissing, "这个页面还没指定站点工程，评论无法显示。"),
		EntityMissing:  r.tr(rfenums.CommentEntityMissing, "这个页面还没指定评论对象（实体类型与实体 id）。"),
		EntityInvalid:  r.tr(rfenums.CommentEntityInvalid, "这个页面的评论对象类型不被支持，请联系站点管理员。"),
		ListFailed:     r.tr(rfenums.CommentListFailed, "评论暂时读不出来 —— 稍后刷新页面再试。"),
		BodyRequired:   r.tr(rfenums.CommentBodyRequired, "评论内容不能为空。"),
		BodyTooLong:    r.tr(rfenums.CommentBodyTooLong, "评论太长了（最多 2000 字）。"),
		CSRFExpired:    r.tr(rfenums.CommentCSRFExpired, "页面已过期，请刷新后重试。"),
		RateLimited:    r.tr(rfenums.CommentRateLimited, "评论太频繁了，休息一会儿再发。"),
		SubmitFailed:   r.tr(rfenums.CommentSubmitFailed, "评论没能提交成功，请稍后重试。"),
	}
}

// renderCommentList 评论列表（已通过）+ 提交表单 / 登录引导。
func renderCommentList(ctx context.Context, r *Request) (string, error) {
	data := commentFragmentData{
		Fragment:   commentFragmentList,
		MaxBodyLen: commentMaxBodyLen,
		ProjectID:  paramOf(r, "projectId"),
		EntityType: paramOf(r, "entityType"),
		EntityID:   paramOf(r, "entityId"),
		CSRFToken:  strings.TrimSpace(r.CSRFToken),
		Labels:     commentLabelsOf(r),
	}
	page := 1
	if n, err := strconv.Atoi(strings.TrimSpace(paramOf(r, "page"))); err == nil && n > 0 {
		page = n
	}
	data.Page = page

	// 降级顺序即优先级：端口没接（运维）> 页面没配（编辑）> 读失败（可重试）。
	switch {
	case deps.CommentPort == nil:
		data.Unavailable = true
		data.Notice = data.Labels.Unavailable
	case strings.TrimSpace(data.ProjectID) == "":
		data.ConfigMissing = true
		data.Notice = data.Labels.ProjectMissing
	case strings.TrimSpace(data.EntityType) == "" || strings.TrimSpace(data.EntityID) == "":
		data.ConfigMissing = true
		data.Notice = data.Labels.EntityMissing
	default:
		res, err := deps.CommentPort.ListApproved(ctx, &commentdto.ListReq{
			ProjectID:  data.ProjectID,
			EntityType: data.EntityType,
			EntityID:   data.EntityID,
			Page:       page,
			PageSize:   commentPageSize,
		})
		if err != nil || res == nil {
			data.Failed = true
			data.Notice = commentFacingText(r, err, data.Labels.ListFailed)
		} else {
			data.Items = commentItemViewsOf(res.Items, data.Labels.AuthorGuest)
			data.HasItems = len(data.Items) > 0
			data.Total = res.Total
			data.Page = res.Page
			data.HasMore = res.HasMore
			if res.HasMore {
				data.NextPage = res.Page + 1
			}
		}
	}

	// 提交入口的可用性（列表是否降级不影响这一块的可读性，但它自己也有前提）。
	userID, loggedIn := visitorIDOf(r)
	switch {
	case !loggedIn:
		// 未登录：列表照常显示（列表是公开的），另给一句登录引导 ——
		// **不是** 401（见文件头：HTMX 不替换 401，访客什么都看不到）。
		data.NeedLogin = true
		if data.Notice == "" {
			data.Notice = data.Labels.Guest
		}
	case data.CSRFToken == "":
		// 已登录但拿不到访客域 token：渲染一个必然 403 的表单是最糟的形态
		// （用户填完点提交，得到一句「CSRF 校验失败」）。改为一句说明。
		data.NoToken = true
		if data.Notice == "" {
			data.Notice = r.tr(rfenums.CommonSessionNotReady, "会话未就绪，请刷新页面后重试。")
		}
	default:
		data.CanSubmit = true
	}
	_ = userID

	return templates.RenderFragment("comment_list", data)
}

// renderCommentSubmit 提交评论（落 pending，返回可见文案）。
func renderCommentSubmit(ctx context.Context, r *Request) (string, error) {
	labels := commentLabelsOf(r)
	data := commentSubmitData{Fragment: commentFragmentSubmit, Labels: labels}

	projectID := paramOf(r, "projectId")
	entityType := paramOf(r, "entityType")
	entityID := paramOf(r, "entityId")
	body := paramOf(r, "body")
	parentID := int64(0)
	if n, err := strconv.ParseInt(strings.TrimSpace(paramOf(r, "parentId")), 10, 64); err == nil && n > 0 {
		parentID = n
	}

	// 未登录：给引导，**不是** 401（见文件头）。顺序在 CSRF 之前是刻意的 ——
	// 未登录的访客本来就不该填评论，先告诉他去登录比先告诉他「页面过期」更有用。
	userID, loggedIn := visitorIDOf(r)
	switch {
	case deps.CommentPort == nil:
		data.Notice = labels.Unavailable
	case !loggedIn:
		data.Notice = labels.Guest
	case !validCommentCSRF(r):
		// CSRF 纵深：跨站伪造的表单拿不到 cookie 里的 token 值（SameSite=Lax 下
		// 跨站 POST 连 cookie 都不带），所以正常提交必然命中；命中不了的一律拒绝。
		data.Notice = labels.CSRFExpired
	default:
		res, err := deps.CommentPort.Submit(ctx, &commentdto.SubmitReq{
			ProjectID:  projectID,
			EntityType: entityType,
			EntityID:   entityID,
			UserID:     userID,
			ParentID:   parentID,
			Body:       body,
			IPHash:     commentSourceHash(r.IP),
		})
		if err != nil || res == nil {
			data.Notice = commentFacingText(r, err, labels.SubmitFailed)
		} else {
			data.OK = true
			data.Notice = labels.PendingNotice
		}
	}

	return templates.RenderFragment("comment_submit", data)
}

// validCommentCSRF 校验访客域的 CSRF token。
//
// 判据是**常数时间比较**（crypto/subtle）：逐字节比较会让「第几位开始不同」
// 变成可观测的时间差，而 token 是唯一的防线之一。
//
// 两边都为空时返回 false（fail-closed）：空 token 说明片段端点没挂访客身份中间件，
// 此时**没有任何凭据能证明这个请求来自本站页面**，放行等于把写入口敞开。
func validCommentCSRF(r *Request) bool {
	expected := strings.TrimSpace(r.CSRFToken)
	if expected == "" {
		return false
	}
	got := strings.TrimSpace(paramOf(r, "csrf_token"))
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// commentFacingText 把评论模块的错误转成一句可展示文案（拿不到出口时用本地兜底）。
//
// err 为 nil 时直接给兜底文案：走到这里说明 res 也是 nil（契约承诺「失败返回业务错误」，
// 但片段层不押注在调用方一定这么做 —— 一个 nil 结果静默渲染成空列表，
// 访客会以为评论被删了）。
func commentFacingText(r *Request, err error, fallback string) string {
	if deps.CommentFacingTexter == nil || err == nil {
		return fallback
	}
	if text := strings.TrimSpace(deps.CommentFacingTexter.FacingText(r.Lang, err)); text != "" {
		return text
	}
	return fallback
}

// commentItemViewsOf 把 dto 列表转成模板视图（组两级：顶层 + 它的一级回复）。
func commentItemViewsOf(items []commentdto.Item, authorFallback string) []commentItemView {
	out := make([]commentItemView, 0, len(items))
	for _, item := range items {
		view := commentItemViewOf(item, authorFallback)
		for _, reply := range item.Replies {
			view.Replies = append(view.Replies, commentItemViewOf(reply, authorFallback))
		}
		out = append(out, view)
	}
	return out
}

// commentItemViewOf 单条转换。
//
// 作者名恒为**兜底文案**：真实显示名在 user 模块手里（访客账号），
// 本批没有接入「按 id 批量取显示名」的收窄端口（见 commentdto.Item 的注释）。
// 显示「访客」而不是留空：空串在页面上就是一块空白，而空白比一句「我们不知道是谁」更难解释。
func commentItemViewOf(item commentdto.Item, authorFallback string) commentItemView {
	return commentItemView{
		ID:        item.ID,
		Body:      item.Body,
		Author:    authorFallback,
		CreatedAt: item.CreatedAt.Time().Format("2006-01-02 15:04"),
		IsReply:   item.IsReply,
	}
}
