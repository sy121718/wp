package userhttp

import (
	"strings"

	"github.com/gin-gonic/gin"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/utils"
	"net/http"
	"net/url"
)

// customer_handle.go — 后台客户管理页（GET /admin/customers 列表、GET /admin/customers/detail 详情）。
//
// 页面与 /api/customer/* 同住 user 模块：写动作复用同一条 Casbin 权限点（user:customer_*，
// 迁移 152），页面拿到的收窄契约（CustomerAdminPort）与 API 侧是同一份实现。
//

// 补齐的空白：访客账号（user 模块）注册 / 验证 / 登录 / 账号中心全都可用，
//
// users 表字段也齐全，但后台此前**没有任何地方读它** —— 管理员看不到客户、
//
// 不能按客户看订单、不能停用或解锁账号。本文件是那个入口。
//
// 四条约定：
//
//  1. 跨模块只依赖 usercontract.CustomerAdminPort（四条方法）与 ordercontract.
//
//     CustomerOrderSummaryReader（一条方法）以及它们的不可变 dto，
//
//     **不 import 任一模块的 model / service**：后台账号的状态取值在这里另有一份
//
//     展示用字面量表（customerStatusViews），真值始终在 users.status。
//
//  2. 订单摘要是**另一个模块的数据**，经订单模块的只读聚合取，绝不 join。
//
//     订单是工程维度的（users 没有工程概念），所以详情页要先选工程 ——
//
//     拿全站订单去对某个客户算「累计消费」，在多工程站点上是个没有意义的数字。
//
//  3. 「停用」与「锁定」在界面上是两件事，文案必须分开：停用是管理动作（不会自己解除），
//
//     锁定是连续登录失败触发的临时状态（到点自动结束）。混成一句话，
//
//     运营会在客户只是「多输错几次密码」时把人停用掉。
//
//  4. ?err= / ?ok= 一律过白名单回显（查询参数是用户可编辑的，不能拿它当业务提示直接显示）。

const (
	// customerPageTitle / customerDetailPageTitle 页面标题（字面量走 shell.Prepare 的
	// fallback 链路，与订单页同口径，不改动 user enums 那个文件的既有集合）。
	customerPageTitle       = "客户管理"
	customerDetailPageTitle = "客户详情"
	// customerEmptyField 空字段的展示占位（表格里的空白单元格读不出「没有值」）。
	customerEmptyField = "—"
	// customerListPath / customerDetailPath 本页两个路径（重定向与链接都从这里取）。
	customerListPath   = "/admin/customers"
	customerDetailPath = "/admin/customers/detail"
)

// 账号状态取值（展示用字面量，刻意不 import user 模块的 model 包 —— 真值在 users.status）。
//
// customerStatusAll 用 -1：0 是「已停用」这个合法筛选值，拿 0 当「全部」
// 会永远筛不出停用账号（而它看起来完全正常）。
const (
	customerStatusAll      = -1
	customerStatusDisabled = 0
	customerStatusActive   = 1
	customerStatusPending  = 2
)

// customerLocalMessages 本页自造的文案（回显白名单的第二部分）。
//
// userenums.UserFacingMessages 只登记「用户模块产出的」文案，而下面这几条是
// 后台页面自己造的参数级提示。它们同样会进 ?err= / ?ok=，也就同样要被回显白名单放行 ——
// 不登记的话，运营看到的是「系统内部错误，请稍后重试」，而真实原因（编号不合法 /
// 能力没装）被自己吞掉了。
const (
	customerUnavailableText       = "客户管理能力未装配（装配缺陷），本页只显示列表框架。"
	customerInvalidIDText         = "客户编号不合法，请回到列表页重新操作。"
	customerOrdersUnavailableText = "订单摘要不可用：订单模块未装配（装配缺陷），客户资料本身不受影响。"
)

// customerLocalMessageSet 本页自造文案的集合（查表用）。
var customerLocalMessageSet = map[string]struct{}{
	customerUnavailableText:       {},
	customerInvalidIDText:         {},
	customerOrdersUnavailableText: {},
}

// customerStatusViews 状态 → 中文标签 + 徽章样式 + 筛选链接用的取值。
var customerStatusViews = []struct {
	Value int
	Label string
	Badge string
}{
	{customerStatusAll, "全部", "badge-mute"},
	{customerStatusActive, "正常", "badge-success"},
	{customerStatusDisabled, "已停用", "badge-danger"},
	{customerStatusPending, "待激活", "badge-warning"},
}

// customerEmailVerifiedViews 邮箱验证筛选（0 = 全部，取值口径与 userdto 一致）。
var customerEmailVerifiedViews = []struct {
	Value int
	Label string
}{
	{userdto.EmailVerifiedAll, "全部"},
	{userdto.EmailVerifiedYes, "已验证"},
	{userdto.EmailVerifiedNo, "未验证"},
}

// customerPageHandle 客户管理页处理器。
type customerPageHandle struct {
	// users 用户模块的后台面（读列表 / 读详情 / 停用启用 / 解除锁定）。
	users usercontract.CustomerAdminPort
	// orders 订单模块的只读聚合 —— 只取「按客户算订单」那一条方法，
	// 不是整个 OrderService（详情页不需要订单列表与状态流转）。
	orders ordercontract.CustomerOrderSummaryReader
	// projects 工程清单：订单摘要按工程统计，详情页要给出工程选择。
	projects projectcontract.ProjectService
}

// NewCustomerPageHandle 构造。
func NewCustomerPageHandle(
	users usercontract.CustomerAdminPort,
	orders ordercontract.CustomerOrderSummaryReader,
	projects projectcontract.ProjectService,
) *customerPageHandle {
	return &customerPageHandle{users: users, orders: orders, projects: projects}
}

// customerFilter 列表页的筛选条件（GET 参数，全部可选）。
type customerFilter struct {
	Keyword        string
	Status         int
	EmailVerified  int
	RegisteredFrom string // 原样保留（date 字符串，回显与回跳都用它）
	RegisteredTo   string
}

// CustomersPage 客户列表（GET /admin/customers）。
func (h *customerPageHandle) CustomersPage(c *gin.Context) {
	ctx := c.Request.Context()
	page, limit := shell.PageParams(c)
	filter := customerFilter{
		Keyword:        strings.TrimSpace(c.Query("keyword")),
		Status:         customerPageStatus(c.Query("status")),
		EmailVerified:  customerPageEmailVerified(c.Query("emailVerified")),
		RegisteredFrom: strings.TrimSpace(c.Query("registeredFrom")),
		RegisteredTo:   strings.TrimSpace(c.Query("registeredTo")),
	}

	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), customerFacingText)
	pageOk := shell.FacingQueryText(c.Query("ok"), "", customerFacingText)

	var list *userdto.CustomerListResp
	switch {
	case h.users == nil:
		// 能力未装配：给出说明而不是 500，也不渲染一个点了必然失败的按钮。
		pageErr = customerFirstNonEmpty(pageErr, customerUnavailableText)
	default:
		res, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			Keyword:        filter.Keyword,
			Status:         filter.Status,
			EmailVerified:  filter.EmailVerified,
			RegisteredFrom: utils.NewJSONTimePtr(customerPageDayStart(filter.RegisteredFrom)),
			RegisteredTo:   utils.NewJSONTimePtr(customerPageDayEnd(filter.RegisteredTo)),
			Offset:         (page - 1) * limit,
			Limit:          limit,
		})
		if err != nil {
			pageErr = customerFirstNonEmpty(pageErr, customerFacingError(c, err))
		} else {
			list = res
		}
	}

	data := shell.Prepare(c, customerListPageData(list, filter, page, limit, pageErr, pageOk, h.users == nil))
	base := shell.FilterBaseURL(customerListPath, customerFilterValues(filter))
	for k, v := range shell.BuildPagination(customerTotal(list), page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/customers.html", data)
}

// CustomerDetailPage 客户详情（GET /admin/customers/detail?id=）。
//
// 资料与登录事实来自 user 模块，订单摘要来自订单模块 —— 页面只做编排。
func (h *customerPageHandle) CustomerDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := customerQueryID(c.Query("id"))
	if id == 0 {
		customerRedirect(c, "", customerInvalidIDText)
		return
	}
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), customerFacingText)
	pageOk := shell.FacingQueryText(c.Query("ok"), "", customerFacingText)

	if h.users == nil {
		c.HTML(http.StatusOK, "admin/customer_detail.html", shell.Prepare(c, customerDetailPageData(
			nil, nil, "", nil, false, false, customerFirstNonEmpty(pageErr, customerUnavailableText), pageOk, c)))
		return
	}

	detail, err := h.users.GetCustomer(ctx, id)
	// detail 为 nil 与 err 一样处理：契约的语义是「不存在返回业务错误」，
	// 但页面不能押注在调用方一定这么做 —— 少了这一条，一个 nil 详情就会渲染出一页
	// 全是空值和必然失败按钮的「客户」。
	if err != nil || detail == nil {
		// 客户不存在时回列表页而不是渲染空详情页：详情页的每个动作都要求一个存在的客户。
		msg := customerFacingError(c, err)
		if msg == "" {
			msg = userenums.ErrUserNotFound
		}
		customerRedirect(c, "", msg)
		return
	}

	// 工程：订单是工程维度的，摘要必须落在某一个工程上（默认第一个，与订单页同口径）。
	var projects []projectcontract.ProjectResp
	projectsErr := ""
	if h.projects != nil {
		list, perr := h.projects.List(ctx)
		if perr != nil {
			projectsErr = shell.PageInternalText(c)
		} else {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var summary *orderdto.CustomerOrderSummaryResp
	summaryErr := ""
	switch {
	case h.orders == nil:
		summaryErr = customerOrdersUnavailableText
	case selected == "":
		// 没有工程就没有订单可算 —— 这是「还没建站点工程」的正常状态，不是错误。
		summaryErr = ""
	default:
		res, oerr := h.orders.CustomerOrderSummaryOf(ctx, &orderdto.CustomerOrderSummaryReq{
			ProjectID: selected,
			UserID:    id,
		})
		if oerr != nil {
			summaryErr = customerFacingError(c, oerr)
		} else {
			summary = res
		}
	}

	c.HTML(http.StatusOK, "admin/customer_detail.html", shell.Prepare(c, customerDetailPageData(
		detail, projects, selected, summary, projectsErr != "", summaryErr != "",
		customerFirstNonEmpty(pageErr, projectsErr, summaryErr), pageOk, c)))
}

// CustomerStatusSave 启用 / 停用账号（POST /admin/customers/status）。
func (h *customerPageHandle) CustomerStatusSave(c *gin.Context) {
	if h.users == nil {
		customerRedirect(c, "", customerUnavailableText)
		return
	}
	id := customerQueryID(c.PostForm("customerId"))
	// 目标状态取 toStatus，**不是** status：表单里的 status 用来保留列表筛选条件，
	// 两者同名会让回跳后的列表看起来「筛选没了」——而它只是刷新了一下。
	status := customerPageStatus(c.PostForm("toStatus"))
	if id == 0 {
		customerRedirect(c, "", customerInvalidIDText)
		return
	}
	// 目标状态只接受「正常 / 已停用」两个值（与 service 的白名单一致，但这里先拦一道）：
	// 表单是客户端可伪造的，而待激活一旦能当目标值，就会出现「被手工改成未验证」的账号 ——
	// 它既收不到验证邮件、也没有人能解释它是怎么来的。
	if status != customerStatusActive && status != customerStatusDisabled {
		customerRedirect(c, "", userenums.ErrCustomerStatusInvalid)
		return
	}
	res, err := h.users.SetCustomerStatus(c.Request.Context(), &userdto.CustomerStatusReq{
		CustomerID: id,
		Status:     status,
	})
	if err != nil {
		customerRedirect(c, "", customerFacingError(c, err))
		return
	}
	// 回执按**目标状态**给：运营点的是「停用」，回执就应该是「账号已停用」。
	customerRedirect(c, customerStatusMessage(res.Status), "")
}

// CustomerUnlock 解除登录锁定（POST /admin/customers/unlock）。
func (h *customerPageHandle) CustomerUnlock(c *gin.Context) {
	if h.users == nil {
		customerRedirect(c, "", customerUnavailableText)
		return
	}
	id := customerQueryID(c.PostForm("customerId"))
	if id == 0 {
		customerRedirect(c, "", customerInvalidIDText)
		return
	}
	res, err := h.users.UnlockCustomer(c.Request.Context(), &userdto.CustomerUnlockReq{CustomerID: id})
	if err != nil {
		customerRedirect(c, "", customerFacingError(c, err))
		return
	}
	// 三种结果各说各的（见 dto 注释）：解除了锁定 / 清了残留计数 / 本来就没事。
	switch {
	case res.Unlocked:
		customerRedirect(c, userenums.MsgCustomerUnlocked, "")
	case res.Cleared:
		customerRedirect(c, userenums.MsgCustomerFailuresCleared, "")
	default:
		customerRedirect(c, userenums.MsgCustomerNotLocked, "")
	}
}

// —— 视图组装（模板不做判断与算术）——

// customerRedirect 回列表页或详情页并把结论经查询参数回显（成功 ?ok=、失败 ?err=）。
//
// 带了 customerId 就回详情页：运营是在某个客户的页面上点的按钮，
// 把他弹回未筛选的列表第一页，等于让他重新找一遍那个客户。
func customerRedirect(c *gin.Context, okText, errText string) {
	q := url.Values{}
	// 回跳要保留的：页面位置（customerId 决定回哪个页面）与筛选条件。
	for _, key := range []string{"customerId", "keyword", "status", "emailVerified",
		"registeredFrom", "registeredTo", "page", "limit", "project"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			q.Set(key, v)
		}
	}
	if okText != "" {
		q.Set("ok", okText)
	}
	if errText != "" {
		q.Set("err", errText)
	}
	target := customerListPath
	if strings.TrimSpace(c.PostForm("customerId")) != "" {
		target = customerDetailPath
	}
	c.Redirect(http.StatusFound, target+"?"+q.Encode())
}
