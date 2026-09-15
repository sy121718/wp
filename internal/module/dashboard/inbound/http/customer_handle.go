// customer_handle.go — 后台客户管理页（GET /admin/customers 列表、GET /admin/customers/detail 详情）。
//
// 补齐的空白：访客账号（user 模块）注册 / 验证 / 登录 / 账号中心全都可用，
// users 表字段也齐全，但后台此前**没有任何地方读它** —— 管理员看不到客户、
// 不能按客户看订单、不能停用或解锁账号。本文件是那个入口。
//
// 四条约定：
//
//  1. 跨模块只依赖 usercontract.CustomerAdminPort（四条方法）与 ordercontract.
//     CustomerOrderSummaryReader（一条方法）以及它们的不可变 dto，
//     **不 import 任一模块的 model / service**：后台账号的状态取值在这里另有一份
//     展示用字面量表（customerStatusViews），真值始终在 users.status。
//
//  2. 订单摘要是**另一个模块的数据**，经订单模块的只读聚合取，绝不 join。
//     订单是工程维度的（users 没有工程概念），所以详情页要先选工程 ——
//     拿全站订单去对某个客户算「累计消费」，在多工程站点上是个没有意义的数字。
//
//  3. 「停用」与「锁定」在界面上是两件事，文案必须分开：停用是管理动作（不会自己解除），
//     锁定是连续登录失败触发的临时状态（到点自动结束）。混成一句话，
//     运营会在客户只是「多输错几次密码」时把人停用掉。
//
//  4. ?err= / ?ok= 一律过白名单回显（查询参数是用户可编辑的，不能拿它当业务提示直接显示）。
package dashboardhttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go_wp/pkg/sitetz"

	"github.com/gin-gonic/gin"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
)

const (
	// customerPageTitle / customerDetailPageTitle 页面标题（字面量走 withI18n 的
	// fallback 链路，与订单页同口径，不改动 dashboard enums 那个文件的既有集合）。
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
// dashboard 自己造的参数级提示。它们同样会进 ?err= / ?ok=，也就同样要被回显白名单放行 ——
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
	page, limit := pageParams(c)
	filter := customerFilter{
		Keyword:        strings.TrimSpace(c.Query("keyword")),
		Status:         customerQueryStatus(c.Query("status")),
		EmailVerified:  customerQueryEmailVerified(c.Query("emailVerified")),
		RegisteredFrom: strings.TrimSpace(c.Query("registeredFrom")),
		RegisteredTo:   strings.TrimSpace(c.Query("registeredTo")),
	}

	pageErr := customerQueryText(c, c.Query("err"), customerInternalText(c))
	pageOk := customerQueryText(c, c.Query("ok"), "")

	var list *userdto.CustomerListResp
	switch {
	case h.users == nil:
		// 能力未装配：给出说明而不是 500，也不渲染一个点了必然失败的按钮。
		pageErr = firstNonEmpty(pageErr, customerUnavailableText)
	default:
		res, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			Keyword:        filter.Keyword,
			Status:         filter.Status,
			EmailVerified:  filter.EmailVerified,
			RegisteredFrom: customerDayStart(filter.RegisteredFrom),
			RegisteredTo:   customerDayEnd(filter.RegisteredTo),
			Offset:         (page - 1) * limit,
			Limit:          limit,
		})
		if err != nil {
			pageErr = firstNonEmpty(pageErr, customerFacingError(c, err))
		} else {
			list = res
		}
	}

	data := withCSRF(c, customerListPageData(list, filter, page, limit, pageErr, pageOk, h.users == nil))
	base := filterBaseURL(customerListPath, customerFilterValues(filter))
	for k, v := range buildPagination(customerTotal(list), page, limit, base, translateFor(c)).templateKeys() {
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
	pageErr := customerQueryText(c, c.Query("err"), customerInternalText(c))
	pageOk := customerQueryText(c, c.Query("ok"), "")

	if h.users == nil {
		c.HTML(http.StatusOK, "admin/customer_detail.html", withCSRF(c, customerDetailPageData(
			nil, nil, "", nil, false, false, firstNonEmpty(pageErr, customerUnavailableText), pageOk, c)))
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
			projectsErr = customerInternalText(c)
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

	c.HTML(http.StatusOK, "admin/customer_detail.html", withCSRF(c, customerDetailPageData(
		detail, projects, selected, summary, projectsErr != "", summaryErr != "",
		firstNonEmpty(pageErr, projectsErr, summaryErr), pageOk, c)))
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
	status := customerQueryStatus(c.PostForm("toStatus"))
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

// customerListPageData 组装列表页渲染数据（纯函数：不取数、不依赖 gin.Context 之外的东西）。
//
// 抽出来的理由同订单页：渲染键名与计数口径只在这里定义一次，
// 真实渲染测试可以直接喂数据走同一条组装路径，不必在测试里手抄一份键名
// （手抄的那份会随模板演进静默失配，而那正是「页面上少了一块、断言却通过」的成因）。
func customerListPageData(list *userdto.CustomerListResp, filter customerFilter,
	page, limit int, pageErr, pageOk string, capabilityMissing bool) gin.H {
	rows := make([]gin.H, 0)
	counters := userdto.CustomerCounters{}
	if list != nil {
		counters = list.Counters
		for _, item := range list.List {
			rows = append(rows, customerRow(item))
		}
	}
	return gin.H{
		"title":             customerPageTitle,
		"menu":              "customers",
		"Rows":              rows,
		"Total":             customerTotal(list),
		"Page":              page,
		"Limit":             limit,
		"Counters":          counters,
		"StatusOptions":     customerStatusOptions(filter.Status),
		"VerifiedOptions":   customerVerifiedOptions(filter.EmailVerified),
		"FilterKeyword":     filter.Keyword,
		"FilterStatus":      filter.Status,
		"FilterVerified":    filter.EmailVerified,
		"FilterFrom":        filter.RegisteredFrom,
		"FilterTo":          filter.RegisteredTo,
		"CapabilityMissing": capabilityMissing,
		"Err":               pageErr,
		"Ok":                pageOk,
	}
}

// customerDetailPageData 组装详情页渲染数据。
//
// 四个「给运营看的说明」都是显式布尔 + 文案，而不是让模板去判断 nil：
// Jet 里判断一个可能为 nil 的接口值很容易写成「看起来对、渲染出来是空块」。
func customerDetailPageData(detail *userdto.CustomerResp, projects []projectcontract.ProjectResp,
	selected string, summary *orderdto.CustomerOrderSummaryResp,
	projectsFailed, summaryFailed bool, pageErr, pageOk string, c *gin.Context) gin.H {
	data := gin.H{
		"title":           customerDetailPageTitle,
		"menu":            "customers",
		"Err":             pageErr,
		"Ok":              pageOk,
		"HasDetail":       detail != nil,
		"Projects":        projects,
		"SelectedProject": selected,
		"HasProjects":     len(projects) > 0,
		"ProjectsFailed":  projectsFailed,
		"SummaryFailed":   summaryFailed,
		"HasSummary":      summary != nil,
		"ListURL":         customerDetailBackURL(c),
	}
	if detail != nil {
		row := customerRow(detail)
		data["Customer"] = row
		data["ID"] = detail.ID
		data["Username"] = detail.Username
		data["Email"] = detail.Email
		data["DisplayLabel"] = row["DisplayLabel"]
		data["StatusLabel"] = detail.StatusLabel
		data["StatusBadge"] = row["StatusBadge"]
		data["EmailVerified"] = detail.EmailVerified
		data["EmailVerifiedLabel"] = row["EmailVerifiedLabel"]
		data["RegisteredAtText"] = detail.RegisteredAtText
		data["RegisterIP"] = row["RegisterIP"]
		data["RegisterLocation"] = row["RegisterLocation"]
		data["LastLoginTimeText"] = detail.LastLoginTimeText
		data["LastLoginIP"] = row["LastLoginIP"]
		data["LastLoginLocation"] = row["LastLoginLocation"]
		data["Locked"] = detail.Locked
		data["LockedUntilText"] = row["LockedUntilText"]
		data["LoginFailureCount"] = detail.LoginFailureCount
		data["Actionable"] = row["Actionable"]
		data["PendingHint"] = row["PendingHint"]
		data["NextStatus"] = row["NextStatus"]
		data["StatusActionLabel"] = row["StatusActionLabel"]
		// 锁定提示分开给：锁定的账号「登不上去」但状态是正常的，
		// 这两件事在页面上必须能分辨（否则运营会去点停用）。
		data["LockHint"] = row["LockHint"]
	}
	if summary != nil {
		data["OrderCount"] = summary.OrderCount
		data["PaidOrderCount"] = summary.PaidOrderCount
		data["TotalAmountLabel"] = summary.TotalAmountLabel
		data["LastOrderNo"] = summary.LastOrderNo
		// 原样传：模板靠它是否为空来区分「有最近一单」与「还没下过单」，
		// 在这里转成「—」会让两种状态长得一模一样（于是零订单显示成一行破折号）。
		data["LastOrderTimeText"] = summary.LastOrderTimeText
		data["LastOrderStatusLabel"] = customerOrderStatusLabel(summary.LastOrderStatus)
		// 最近一单的直达链接：订单页按 orderId 参数展开详情（不新开路由）。
		data["LastOrderURL"] = customerOrderURL(selected, summary.LastOrderID)
	} else {
		data["HasSummary"] = false
	}
	return data
}

// customerRow 一行客户（列表与详情共用同一份事实；两种页面看到的数字因此不可能不一致）。
func customerRow(item *userdto.CustomerResp) gin.H {
	row := gin.H{
		"ID":                 item.ID,
		"Username":           customerTextOrEmpty(item.Username),
		"Email":              customerTextOrEmpty(item.Email),
		"DisplayLabel":       customerDisplayLabel(item),
		"Status":             item.Status,
		"StatusLabel":        customerTextOrEmpty(item.StatusLabel),
		"StatusBadge":        customerStatusBadge(item.Status),
		"EmailVerified":      item.EmailVerified,
		"EmailVerifiedLabel": customerVerifiedLabel(item.EmailVerified),
		"RegisteredAtText":   customerTextOrEmpty(item.RegisteredAtText),
		"RegisterIP":         customerTextOrEmpty(item.RegisterIP),
		"RegisterLocation":   customerTextOrEmpty(item.RegisterLocation),
		"LastLoginTimeText":  customerTextOrEmpty(item.LastLoginTimeText),
		"LastLoginIP":        customerTextOrEmpty(item.LastLoginIP),
		"LastLoginLocation":  customerTextOrEmpty(item.LastLoginLocation),
		"Locked":             item.Locked,
		"LockedUntilText":    customerTextOrEmpty(item.LockedUntilText),
		"LoginFailureCount":  item.LoginFailureCount,
		"DetailURL":          customerDetailURL(item.ID),
		// 状态动作：只有「正常 ↔ 已停用」两个方向。
		//
		// 待激活的账号刻意不给按钮：它登不上去（status != active 一律拒绝登录），
		// 停用它只会让客户点验证链接时得到「链接失效」—— 那既没解决问题，
		// 又让客户来问「为什么我的链接坏了」。
		"Actionable": false,
	}
	switch item.Status {
	case customerStatusActive:
		row["Actionable"] = true
		row["NextStatus"] = customerStatusDisabled
		row["StatusActionLabel"] = "停用"
	case customerStatusDisabled:
		row["Actionable"] = true
		row["NextStatus"] = customerStatusActive
		row["StatusActionLabel"] = "启用"
	case customerStatusPending:
		row["PendingHint"] = "待激活：客户还没完成邮箱验证。这类账号本来就登不上去，" +
			"客户验证完邮箱后状态会变成「正常」，那时再决定是否停用。"
	}
	if item.Locked {
		row["LockHint"] = "该账号因连续登录失败被临时锁定（到点会自动解除），客户目前登不上去 —— " +
			"如果确认是本人操作，点「解除锁定」让他不用等。"
	} else if item.LoginFailureCount > 0 {
		row["LockHint"] = "该账号有未清零的登录失败次数：再失败几次就会进入锁定。"
	}
	return row
}

// customerStatusOptions 状态下拉（含「全部」，当前值预选）。
func customerStatusOptions(selected int) []gin.H {
	out := make([]gin.H, 0, len(customerStatusViews))
	for _, v := range customerStatusViews {
		out = append(out, gin.H{"Value": v.Value, "Label": v.Label, "Selected": v.Value == selected})
	}
	return out
}

// customerVerifiedOptions 邮箱验证下拉。
func customerVerifiedOptions(selected int) []gin.H {
	out := make([]gin.H, 0, len(customerEmailVerifiedViews))
	for _, v := range customerEmailVerifiedViews {
		out = append(out, gin.H{"Value": v.Value, "Label": v.Label, "Selected": v.Value == selected})
	}
	return out
}

// customerFilterValues 列表页链接要保留的筛选条件（空值由 filterBaseURL 丢弃）。
func customerFilterValues(filter customerFilter) map[string]string {
	return map[string]string{
		"keyword":        filter.Keyword,
		"status":         customerStatusQueryValue(filter.Status),
		"emailVerified":  customerVerifiedQueryValue(filter.EmailVerified),
		"registeredFrom": filter.RegisteredFrom,
		"registeredTo":   filter.RegisteredTo,
	}
}

// customerStatusQueryValue 状态 → 查询参数值（「全部」不写进 URL：
// ?status=-1 与不带参数是同一件事，带上只会让链接看起来筛过了）。
func customerStatusQueryValue(status int) string {
	if status == customerStatusAll {
		return ""
	}
	return strconv.Itoa(status)
}

// customerVerifiedQueryValue 邮箱验证 → 查询参数值（0 = 全部，不写进 URL）。
func customerVerifiedQueryValue(v int) string {
	if v == userdto.EmailVerifiedAll {
		return ""
	}
	return strconv.Itoa(v)
}

// —— 表单与文案工具 ——

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

// customerDetailBackURL 「返回列表」链接：只保留筛选条件（把详情专属参数留在详情页）。
// c 为 nil 时退化成纯列表路径：渲染数据组装是纯函数，理应在没有请求上下文时也能跑
// （渲染测试就是直接喂数据走这条路径的），不该因为少一个 context 就 panic。
func customerDetailBackURL(c *gin.Context) string {
	q := url.Values{}
	if c != nil {
		for _, key := range []string{"keyword", "status", "emailVerified", "registeredFrom", "registeredTo", "page", "limit"} {
			if v := strings.TrimSpace(c.Query(key)); v != "" {
				q.Set(key, v)
			}
		}
	}
	if len(q) == 0 {
		return customerListPath
	}
	return customerListPath + "?" + q.Encode()
}

// customerOrderURL 最近一单在订单管理页的展开链接（订单页靠 orderId 参数展开详情）。
func customerOrderURL(projectID string, orderID uint64) string {
	if orderID == 0 {
		return ""
	}
	q := url.Values{}
	if strings.TrimSpace(projectID) != "" {
		q.Set("project", projectID)
	}
	q.Set("orderId", strconv.FormatUint(orderID, 10))
	return "/admin/orders?" + q.Encode()
}

// customerDetailURL 某个客户的详情链接（保留不来 —— 返回时用「返回列表」回到筛选结果）。
func customerDetailURL(id uint64) string {
	return customerDetailPath + "?id=" + strconv.FormatUint(id, 10)
}

// customerFacingError 把 user 模块的错误转成可展示文案。
//
// 只放行 userenums.UserFacingMessages 白名单，其余一律落到统一提示：
// 未命中的通常是数据库错误的 Error()，带表名甚至 SQL 片段，那是给运维看的。
func customerFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := customerFacingText(err.Error()); msg != "" {
		return msg
	}
	return customerInternalText(c)
}

// customerFacingText 白名单校验：命中返回原文，未命中返回空串。
func customerFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	for _, allowed := range userenums.UserFacingMessages {
		if msg == allowed {
			return msg
		}
	}
	if _, ok := customerLocalMessageSet[msg]; ok {
		return msg
	}
	return ""
}

// customerQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，
// 未命中时用 fallback（错误提示落统一文案，成功提示落空串）——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func customerQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := customerFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}

// customerInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func customerInternalText(c *gin.Context) string {
	return translateFor(c)(dashboardenums.MsgInternalError, "系统内部错误，请稍后重试")
}

// customerStatusMessage 状态写回执文案。
func customerStatusMessage(status int) string {
	if status == customerStatusActive {
		return userenums.MsgCustomerEnabled
	}
	return userenums.MsgCustomerDisabled
}

// customerStatusBadge 状态 → 徽章样式（未知值给中性徽章：宁可显示得平淡，
// 也不要把一个不认识的状态渲染成成功或失败）。
func customerStatusBadge(status int) string {
	for _, v := range customerStatusViews {
		if v.Value == status && v.Value != customerStatusAll {
			return v.Badge
		}
	}
	return "badge-mute"
}

// customerVerifiedLabel 邮箱验证 → 展示文案。
func customerVerifiedLabel(verified bool) string {
	if verified {
		return "已验证"
	}
	return "未验证"
}

// customerDisplayLabel 客户的展示名（展示名 → 昵称 → 登录名）。
//
// 三级回退：客户可能三个字段只填了一个，列表里显示一列破折号等于没显示这个人是谁。
func customerDisplayLabel(item *userdto.CustomerResp) string {
	switch {
	case strings.TrimSpace(item.DisplayName) != "":
		return item.DisplayName
	case strings.TrimSpace(item.Nickname) != "":
		return item.Nickname
	default:
		return customerTextOrEmpty(item.Username)
	}
}

// customerOrderStatusLabel 订单状态 → 中文（未知值原样返回：宁可显示生值，也不显示空白）。
func customerOrderStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "待付款"
	case "paid":
		return "已付款"
	case "shipped":
		return "已发货"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	case "refunded":
		return "已退款"
	case "":
		return customerEmptyField
	default:
		return status
	}
}

// customerTotal 列表总数（list 为 nil 时是 0）。
func customerTotal(list *userdto.CustomerListResp) int64 {
	if list == nil {
		return 0
	}
	return list.Total
}

// customerTextOrEmpty 空值统一显示成「—」。
func customerTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return customerEmptyField
	}
	return value
}

// customerQueryID 解析 id / customerId（非法即 0）。
func customerQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// customerQueryStatus 解析状态（空串或非法一律「全部」）。
//
// 空串**不能**落成 0：0 是「已停用」，那会让不带参数的请求只看到停用账号。
func customerQueryStatus(raw string) int {
	switch strings.TrimSpace(raw) {
	case "0":
		return customerStatusDisabled
	case "1":
		return customerStatusActive
	case "2":
		return customerStatusPending
	default:
		return customerStatusAll
	}
}

// customerQueryEmailVerified 解析邮箱验证筛选（空串 → 全部）。
func customerQueryEmailVerified(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1":
		return userdto.EmailVerifiedYes
	case "2":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

// customerDayStart / customerDayEnd 日期字符串 → 当天的起止时刻（解析不了返回 nil = 该端不限）。
//
// 结束日期必须扩到当天最后一刻：把 2026-09-30 当成 00:00:00，那一天注册的客户
// 一个都筛不出来，而运营以为自己筛的是「到 9 月 30 日为止」—— 少一天看起来完全正常。
//
// 解析失败按「不限」处理而不是报错：这是筛选条件，拼错了退化成不筛，
// 比让整页变成错误页更接近运营的预期（他至少还看得到列表）。
func customerDayStart(raw string) *time.Time {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}
	// 与 API 侧同一口径（pkg/sitetz）：日期筛选按站点时区解释，不跟随服务器时区。
	day, err := time.ParseInLocation("2006-01-02", v, sitetz.Location())
	if err != nil {
		return nil
	}
	return &day
}

func customerDayEnd(raw string) *time.Time {
	day := customerDayStart(raw)
	if day == nil {
		return nil
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end
}
