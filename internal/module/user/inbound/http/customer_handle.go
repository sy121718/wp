package userhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
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
//     **不 import 任一模块的 model / service**：账号状态的取值与展示名统一取本模块的
//
//     userenums（StatusLabel 给「key + 中文兜底」），真值始终在 users.status ——
//
//     页面不再自造一份「状态 → 文案 / 徽章」的字面量表。
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

// userLabel 一条展示标签 / 自造文案（i18n key + 中文兜底）。本模块通用（客户页、访客页都用）。
//
// 取词只有一个入口（userLabelOf），tr 为 nil（纯函数测试路径）时回落中文兜底。
// 直接在 Go 里写中文的后果是：页面模板里的 {{.Err}} / {{.Ok}} / {{.title}} / {{.message}}
// 都是**直接渲染**的文本、不经过 pkg/response 的 translate —— 英文界面恒中文。
type userLabel struct{ key, fallback string }

// userLabelOf 取一条标签的当前语言文本。
func userLabelOf(tr func(key, fallback string) string, l userLabel) string {
	if tr == nil {
		return l.fallback
	}
	return tr(l.key, l.fallback)
}

const (
	// customerEmptyField 空字段的展示占位（表格里的空白单元格读不出「没有值」）。
	// 值来自 enums：订单状态等标签的兜底同样要用它，两处各写一个「—」会漂移。
	customerEmptyField = userenums.LabelEmptyField
	// customerListPath / customerDetailPath 本页两个路径（重定向与链接都从这里取）。
	customerListPath   = "/admin/customers"
	customerDetailPath = "/admin/customers/detail"
)

// 客户页的页面标题：词条命中出译文，未命中回落中文兜底（经 shell.Prepare 的 fallback 链路进 <title>）。
var (
	customerPageTitleLabel       = userLabel{"admin.customers.heading", "客户管理"}
	customerDetailPageTitleLabel = userLabel{"admin.customer_detail.heading", "客户详情"}
)

// 账号状态取值：真源在本模块 enums（与 usermodel.UserStatus* 同值），页面与 service 共用同一份。
//
// 这里不再各写一遍字面量 —— 页面用 -1 表示「全部」这件事实与展示层无关。
const (
	customerStatusAll      = userenums.StatusAll
	customerStatusDisabled = userenums.StatusDisabled
	customerStatusActive   = userenums.StatusActive
	customerStatusPending  = userenums.StatusPending
)

// 状态按钮的动词词条（列表页按钮与详情页「<动词>这个账号」共用同一份）。
//
// 动词也必须走词条：详情页的按钮是「{{动词}}{{.account_suffix}}」拼出来的，
// 而 account_suffix 早已中英成对（zh「这个账号」/ en「 this account」）——
// 动词留在 Go 里当硬编码中文时，英文界面就变成「Disable这个账号」式中英混排。
const (
	customerActionDisable = "admin.customers.action.disable"
	customerActionEnable  = "admin.customers.action.enable"
)

// customerLocalMessages 本页自造的文案（回显白名单的第二部分）。
//
// userenums.UserFacingMessages 只登记「用户模块产出的」文案，而下面这几条是
// 后台页面自己造的参数级提示。它们同样会进 ?err= / ?ok=，也就同样要被回显白名单放行 ——
// 不登记的话，运营看到的是「系统内部错误，请稍后重试」，而真实原因（编号不合法 /
// 能力没装）被自己吞掉了。
//
// 形态是 key + 中文兜底：**直接渲染**的那些路径按当前语言取词；
// 走 ?err= 回显的那些路径仍落 fallback（中文兜底）—— 读侧白名单认的是这条中文串，
// 取词后的英文译文会被自己吞掉。通道本身的改造（改成传 key + 读侧按键取词）
// 由共享辅助统一做，key 已经备好。
var (
	// customerUnavailableLabel 客户管理能力未装配。
	//
	// 复用库里现成的 admin.customers.capability_missing（模板 customers.html 的
	// 装配提示块用的是同一条词条）：同一件事在一个页面上有两种说法，运营会以为
	// 「页顶那条」与「列表里那条」是两回事。
	customerUnavailableLabel = userLabel{"admin.customers.capability_missing",
		"客户管理能力未装配（装配缺陷），本页只显示列表框架。"}
	// customerInvalidIDLabel 客户编号不合法。
	customerInvalidIDLabel = userLabel{"admin.customers.form.invalid_id",
		"客户编号不合法，请回到列表页重新操作。"}
	// customerOrdersUnavailableLabel 订单摘要不可用（订单模块未装配）。
	customerOrdersUnavailableLabel = userLabel{"admin.customer_detail.orders.unavailable",
		"订单摘要不可用：订单模块未装配（装配缺陷），客户资料本身不受影响。"}
	// customerBulkNothingSelectedLabel 批量操作一条都没勾。
	customerBulkNothingSelectedLabel = userLabel{"admin.customers.bulk.none_selected",
		"批量操作：没有勾选任何账号，请先勾选左侧复选框再执行。"}
)

// customerLocalMessageSet 本页自造文案的集合（查表用）：
// 中文兜底（?err= 通道里传的就是它）与 key 都登记 —— 通道改造后两种形态都能命中。
var customerLocalMessageSet = map[string]struct{}{
	customerUnavailableLabel.fallback:         {},
	customerUnavailableLabel.key:              {},
	customerInvalidIDLabel.fallback:           {},
	customerInvalidIDLabel.key:                {},
	customerOrdersUnavailableLabel.fallback:   {},
	customerOrdersUnavailableLabel.key:        {},
	customerBulkNothingSelectedLabel.fallback: {},
	customerBulkNothingSelectedLabel.key:      {},
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
	// membership 会员身份读取（BIZ-3 展示侧）；membershipFacing 是它的错误文案出口。
	//
	// 二者经 SetMembershipDisplay 成对注入，允许都为 nil：详情页据此渲染一句
	// 「会员模块尚未接入」，而不是一个空白块（缺能力降级成一句人话，同其它页面的口径）。
	// 收窄到 Reader —— 客户页只读等级，没有改等级 / 解锁 / 重算的能力。
	membership       membershipcontract.Reader
	membershipFacing membershipcontract.FacingTexter
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
	Locked         bool   // 只看当前被锁定的账号（与「停用」是两条轴）
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
		Locked:         customerPageLocked(c.Query("locked")),
		RegisteredFrom: strings.TrimSpace(c.Query("registeredFrom")),
		RegisteredTo:   strings.TrimSpace(c.Query("registeredTo")),
	}

	// 回显走 customerPageFacingText（判定 + 取译文）：白名单里是 item_key，
	// 模板 {{.Err}} / {{.Ok}} 直接渲染，只放行 key 就会把裸 key 显示给运营。
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), customerPageFacingText(c))
	pageOk := shell.FacingQueryText(c.Query("ok"), "", customerPageFacingText(c))
	// 展示标签与自造文案的取词函数（不再在 Go 里写死中文）。
	tr := shell.TranslateFor(c)

	var list *userdto.CustomerListResp
	switch {
	case h.users == nil:
		// 能力未装配：给出说明而不是 500，也不渲染一个点了必然失败的按钮。
		pageErr = customerFirstNonEmpty(pageErr, userLabelOf(tr, customerUnavailableLabel))
	default:
		res, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			Keyword:        filter.Keyword,
			Status:         filter.Status,
			EmailVerified:  filter.EmailVerified,
			LockedOnly:     filter.Locked,
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

	data := shell.Prepare(c, customerListPageData(tr, list, filter, page, limit, pageErr, pageOk, h.users == nil))
	// 批量动作的结果摘要经 ?done= 回带（单条动作仍走 ?ok= / ?err=，见 customerBulkRedirect）。
	// 读侧过受控出口（customerPageDone）：查询参数是用户可编辑的，未命中落空串。
	// 可选键：直接渲染模板的单测不带 Done，缺失键会让整页在此中断（HTTP 仍 200）。
	data["Done"] = customerPageDone(c)
	base := shell.FilterBaseURL(customerListPath, customerFilterValues(filter))
	for k, v := range shell.BuildPagination(customerTotal(list), page, limit, base, shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/user/customers.html", data)
}

// CustomerDetailPage 客户详情（GET /admin/customers/detail?id=）。
//
// 资料与登录事实来自 user 模块，订单摘要来自订单模块 —— 页面只做编排。
func (h *customerPageHandle) CustomerDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := customerQueryID(c.Query("id"))
	if id == 0 {
		customerRedirect(c, "", customerInvalidIDLabel.fallback)
		return
	}
	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), customerPageFacingText(c))
	pageOk := shell.FacingQueryText(c.Query("ok"), "", customerPageFacingText(c))
	// 展示标签与自造文案的取词函数（不再在 Go 里写死中文）。
	tr := shell.TranslateFor(c)

	if h.users == nil {
		c.HTML(http.StatusOK, "admin/user/customer_detail.html", shell.Prepare(c, customerDetailPageData(
			nil, nil, "", nil, false, false, customerFirstNonEmpty(pageErr, userLabelOf(tr, customerUnavailableLabel)), pageOk, c)))
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

	var summary *ordercontract.CustomerOrderSummaryResp
	summaryErr := ""
	switch {
	case h.orders == nil:
		summaryErr = userLabelOf(tr, customerOrdersUnavailableLabel)
	case selected == "":
		// 没有工程就没有订单可算 —— 这是「还没建站点工程」的正常状态，不是错误。
		summaryErr = ""
	default:
		res, oerr := h.orders.CustomerOrderSummaryOf(ctx, &ordercontract.CustomerOrderSummaryReq{
			ProjectID: selected,
			UserID:    id,
		})
		if oerr != nil {
			summaryErr = customerFacingError(c, oerr)
		} else {
			summary = res
		}
	}

	// 会员等级（BIZ-3 展示侧）：只加展示、不加领域 —— 等级与权益按「工程 + 客户」解析，
	// 与上面订单摘要用同一个 selected 工程（两个数字必须落在同一个工程上，
	// 否则页面上会出现「工程 A 的消费、工程 B 的等级」，而两者都看起来是对的）。
	// 渲染键在 applyCustomerMembership 里**恒设**（含空值）：Jet 缺键会中断整页渲染。
	memberData := customerDetailPageData(
		detail, projects, selected, summary, projectsErr != "", summaryErr != "",
		customerFirstNonEmpty(pageErr, projectsErr, summaryErr), pageOk, c)
	applyCustomerMembership(memberData, h.membershipView(ctx, c, selected, id))
	c.HTML(http.StatusOK, "admin/user/customer_detail.html", shell.Prepare(c, memberData))
}

// CustomerStatusSave 启用 / 停用账号（POST /admin/customers/status）。
func (h *customerPageHandle) CustomerStatusSave(c *gin.Context) {
	if h.users == nil {
		customerRedirect(c, "", customerUnavailableLabel.fallback)
		return
	}
	id := customerQueryID(c.PostForm("customerId"))
	// 目标状态取 toStatus，**不是** status：表单里的 status 用来保留列表筛选条件，
	// 两者同名会让回跳后的列表看起来「筛选没了」——而它只是刷新了一下。
	status := customerPageStatus(c.PostForm("toStatus"))
	if id == 0 {
		customerRedirect(c, "", customerInvalidIDLabel.fallback)
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
		customerRedirect(c, "", customerUnavailableLabel.fallback)
		return
	}
	id := customerQueryID(c.PostForm("customerId"))
	if id == 0 {
		customerRedirect(c, "", customerInvalidIDLabel.fallback)
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

// CustomerBulkStatusSave 批量启用 / 停用（POST /admin/customers/bulk-status）。
//
// 逐条走**同一条单条写入路径**（h.users.SetCustomerStatus，与 /admin/customers/status 一致）：
// 某一条失败不中断整批 —— 整批回滚会让运营以为「一条都没做」，然后反复重试。
// 结果按「已处理 N 个 / 未处理 M 个」回带，不做静默的部分成功。
func (h *customerPageHandle) CustomerBulkStatusSave(c *gin.Context) {
	if h.users == nil {
		customerRedirect(c, "", customerUnavailableLabel.fallback)
		return
	}
	// 目标状态取自 toStatus（表单里的 status 仍是「保留列表筛选」用的，两者同名会让
	// 回跳后的列表看起来「筛选没了」）；只接受「正常 / 已停用」两个值，与单条动作同口径。
	status := customerPageStatus(c.PostForm("toStatus"))
	if status != customerStatusActive && status != customerStatusDisabled {
		customerRedirect(c, "", userenums.ErrCustomerStatusInvalid)
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		customerRedirect(c, "", customerBulkIDsText(c, berr))
		return
	}
	if len(ids) == 0 {
		// 表单是客户端可伪造的：一条都没勾就直接提交是可能的，不能当成功处理。
		customerRedirect(c, "", customerBulkNothingSelectedLabel.fallback)
		return
	}
	ctx := c.Request.Context()
	done, skipped := 0, 0
	for _, raw := range ids {
		id := customerQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		if _, err := h.users.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{
			CustomerID: id, Status: status,
		}); err != nil {
			skipped++
			continue
		}
		done++
	}
	customerBulkRedirect(c, customerBulkSummary(c, customerStatusActionVerb(c, status), done, skipped))
}

// CustomerBulkUnlock 批量解除登录锁定（POST /admin/customers/bulk-unlock）。
//
// 同一批里三种情况分开计数（真的解开了 / 本来就没事 / 未处理）：
// 把「本来就没事」混进「失败」会让运营以为有账号没解锁成功而去点第二次，
// 混进「成功」则是谎报 —— 而它其实是这条批量指令里最需要被解释的一种结果。
func (h *customerPageHandle) CustomerBulkUnlock(c *gin.Context) {
	if h.users == nil {
		customerRedirect(c, "", customerUnavailableLabel.fallback)
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		customerRedirect(c, "", customerBulkIDsText(c, berr))
		return
	}
	if len(ids) == 0 {
		customerRedirect(c, "", customerBulkNothingSelectedLabel.fallback)
		return
	}
	ctx := c.Request.Context()
	unlocked, noop, skipped := 0, 0, 0
	for _, raw := range ids {
		id := customerQueryID(raw)
		if id == 0 {
			skipped++
			continue
		}
		res, err := h.users.UnlockCustomer(ctx, &userdto.CustomerUnlockReq{CustomerID: id})
		switch {
		case err != nil:
			skipped++
		case res != nil && res.Unlocked:
			unlocked++
		default:
			noop++
		}
	}
	customerBulkRedirect(c, customerBulkUnlockSummary(c, unlocked, noop, skipped))
}

// —— 批量结论的文案源（?done= 通道）——
//
// 形态与 order 模块的 orderBulkText 同构：**key 与中文原文只有这一份**，
// 写侧用它 Sprintf 出整句，读侧 customerBulkNoticeCandidates 用同一批词条
// （当前语言）经 shell.NoticeTemplate 归一后比对 —— 读侧另抄一份中文的后果是静默的：
// 写侧改了措辞，候选就失配，页面上变成「没有这条提示」。
//
// 句子拆成 前缀 + 分段（连接符拼接）+ 句号 三个部件：英文的语序与标点都不同，
// 把整句焊成一条词条就等于按「分支 × 计数组合」抄一份句子表。
type customerBulkText struct{ key, fallback string }

var (
	customerBulkPrefixStatus = customerBulkText{"user.bulk.prefix.status", "批量操作："}
	customerBulkPrefixUnlock = customerBulkText{"user.bulk.prefix.unlock", "批量解除锁定："}
	customerBulkTailNone     = customerBulkText{"user.bulk.tail.none", "没有可处理的账号。"}
	customerBulkJoiner       = customerBulkText{"user.bulk.joiner", "，"}
	customerBulkPeriod       = customerBulkText{"user.bulk.period", "。"}

	customerBulkStatusDone    = customerBulkText{"user.bulk.status.done", "{verb} {count} 个"}
	customerBulkStatusSkipped = customerBulkText{"user.bulk.status.skipped", "{count} 个未处理（账号不存在，或当前状态不允许这个动作）"}
	customerBulkUnlockDone    = customerBulkText{"user.bulk.unlock.done", "已解除锁定 {count} 个"}
	customerBulkUnlockNoop    = customerBulkText{"user.bulk.unlock.noop", "{count} 个本来就未锁定"}
	customerBulkUnlockSkipped = customerBulkText{"user.bulk.unlock.skipped", "{count} 个未处理（账号不存在）"}

	customerBulkVerbEnabled  = customerBulkText{"user.bulk.verb.enabled", "已启用"}
	customerBulkVerbDisabled = customerBulkText{"user.bulk.verb.disabled", "已停用"}
)

// customerBulkTextOf 取一条批量结论文案的当前语言文本，并按命名参数填充 `{name}`。
//
// 占位符是 `{verb}` / `{count}` 这类名字，填充走 pkg/i18n 的 FillTranslate（命名替换）而非
// fmt.Sprintf：词条能在后台被运营改出裸 %，Sprintf 会把它当格式化动词输出乱码；
// 中英词条的占位符顺序不一致时还会静默错配。填完仍有残留时自动回落中文兜底。
func customerBulkTextOf(c *gin.Context, t customerBulkText, kv map[string]string) string {
	return i18n.FillTranslate(shell.TranslateFor(c), t.key, t.fallback, kv)
}

// customerBulkSentence 拼一句批量结论：前缀 + 分段 + 句号（三部分都按当前语言取）。
func customerBulkSentence(c *gin.Context, prefix customerBulkText, parts []string) string {
	if len(parts) == 0 {
		return customerBulkTextOf(c, prefix, nil) + customerBulkTextOf(c, customerBulkTailNone, nil)
	}
	return customerBulkTextOf(c, prefix, nil) +
		strings.Join(parts, customerBulkTextOf(c, customerBulkJoiner, nil)) +
		customerBulkTextOf(c, customerBulkPeriod, nil)
}

// customerBulkSummary 批量状态动作的结果摘要（成功数与未处理数分开说）。
func customerBulkSummary(c *gin.Context, verb string, done, skipped int) string {
	parts := make([]string, 0, 2)
	if done > 0 {
		parts = append(parts, customerBulkTextOf(c, customerBulkStatusDone, map[string]string{
			"verb": verb, "count": strconv.Itoa(done)}))
	}
	if skipped > 0 {
		parts = append(parts, customerBulkTextOf(c, customerBulkStatusSkipped,
			map[string]string{"count": strconv.Itoa(skipped)}))
	}
	return customerBulkSentence(c, customerBulkPrefixStatus, parts)
}

// customerBulkUnlockSummary 批量解锁的结果摘要（解开 / 本来就没事 / 未处理三件事分开说）。
func customerBulkUnlockSummary(c *gin.Context, unlocked, noop, skipped int) string {
	parts := make([]string, 0, 3)
	if unlocked > 0 {
		parts = append(parts, customerBulkTextOf(c, customerBulkUnlockDone,
			map[string]string{"count": strconv.Itoa(unlocked)}))
	}
	if noop > 0 {
		parts = append(parts, customerBulkTextOf(c, customerBulkUnlockNoop,
			map[string]string{"count": strconv.Itoa(noop)}))
	}
	if skipped > 0 {
		parts = append(parts, customerBulkTextOf(c, customerBulkUnlockSkipped,
			map[string]string{"count": strconv.Itoa(skipped)}))
	}
	return customerBulkSentence(c, customerBulkPrefixUnlock, parts)
}

// customerStatusActionVerb 目标状态 → 摘要里的动词（回执按**目标状态**给，与单条动作一致）。
func customerStatusActionVerb(c *gin.Context, status int) string {
	return customerBulkTextOf(c, customerStatusActionVerbKey(status), nil)
}

// customerBulkNoticeCandidates 批量摘要的候选集合（数字归一后整体比对用）。
//
// **由真实写侧函数产出**，不是手抄第二份：写侧 customerBulkSummary / customerBulkUnlockSummary
// 改措辞时候选自动跟着变，读侧不会静默失配 —— 手抄一份的下场是「写侧改了、读侧再也认不出」，
// 而那种失败表现为**成功回执整体消失**（不报错、日志里也没有），最难被发现。
//
// 语言：候选与写侧**取同一批词条、同一条取词路径**（customerBulkTextOf），
// 所以英文界面下写侧塞英文、读侧也按英文比对，两边不会各说各话。
//
// 计数取 {0,1,3} 三种代表值：归一只保留「有几位数」以外的差别，所以更大/更小的计数同样命中。
func customerBulkNoticeCandidates(c *gin.Context) []string {
	out := make([]string, 0, 24)
	add := func(s string) { out = append(out, shell.NoticeTemplate(s)) }
	// 空结果分支（写侧 parts 为空时返回的固定句）。
	add(customerBulkSummary(c, "", 0, 0))
	add(customerBulkUnlockSummary(c, 0, 0, 0))
	counts := []int{0, 1, 3}
	for _, status := range []int{customerStatusActive, customerStatusDisabled} {
		verb := customerStatusActionVerb(c, status)
		for _, done := range counts {
			for _, skipped := range counts {
				if done == 0 && skipped == 0 {
					continue // 与「空结果」等价，已单列
				}
				add(customerBulkSummary(c, verb, done, skipped))
			}
		}
	}
	for _, unlocked := range counts {
		for _, noop := range counts {
			for _, skipped := range counts {
				if unlocked == 0 && noop == 0 && skipped == 0 {
					continue
				}
				add(customerBulkUnlockSummary(c, unlocked, noop, skipped))
			}
		}
	}
	return out
}

// customerStatusActionVerbKey 目标状态 → 摘要里动词的词条（读侧生成候选时用）。
func customerStatusActionVerbKey(status int) customerBulkText {
	if status == customerStatusActive {
		return customerBulkVerbEnabled
	}
	return customerBulkVerbDisabled
}

// customerPageDone 列表页 ?done= 的受控出口。
//
// `?done=` 是**用户可编辑的查询参数**，模板直接渲染它等于「手拼一个 URL 就能在页面上
// 贴一条看起来来自系统的提示」。这里按写侧真实会产出的那几种句子整体比对（数字归一），
// 未命中落空串 —— 成功态没有「必须说点什么」的语义。
//
// 与单条动作的 ?ok= / ?err= 是两条通道：那些是短 token / 白名单文案，本通道是带计数的
// 动态整句（「批量操作：已停用 3 个，1 个未处理（…）。」），所以判定方式不同、不用同一个函数。
func customerPageDone(c *gin.Context) string {
	return shell.FacingNotice(c.Query("done"), customerBulkNoticeCandidates(c))
}

// customerBulkRedirect 批量动作回列表页：结果摘要经 ?done= 回带。
//
// 为什么不复用单条动作的 ?ok= / ?err=：本页的 ?err= 要过一遍面向访客文案白名单
// （见 customerFacingText），而批量摘要是带数字的动态句子，过白名单只会被替换成
// 「系统内部错误」—— 那等于把「有 3 个没做成」这件事吞掉，比不显示更糟。
// 摘要经 ?done= 走独立通道，模板以 {{.Done}} 渲染。
//
// 2026-09 修订：**不能只靠 Jet 的 HTML 转义**。转义只挡「脚本执行」，不挡「伪造系统提示」——
// 手拼 ?done=<任意文案> 同样会以系统口吻显示在页面上。现在读侧过 customerPageDone，
// 按写侧真实产出的句子整体比对（数字归一），未命中落空串。
//
// 恒回列表页（批量动作只在列表页发起），并保留筛选与翻页，让运营回到原来看的那一屏。
func customerBulkRedirect(c *gin.Context, summary string) {
	q := url.Values{}
	for _, key := range []string{"keyword", "status", "emailVerified", "locked",
		"registeredFrom", "registeredTo", "page", "limit"} {
		if v := strings.TrimSpace(c.PostForm(key)); v != "" {
			q.Set(key, v)
		}
	}
	if summary != "" {
		q.Set("done", summary)
	}
	c.Redirect(http.StatusFound, customerListPath+"?"+q.Encode())
}

// —— 视图组装（模板不做判断与算术）——

// customerRedirect 回列表页或详情页并把结论经查询参数回显（成功 ?ok=、失败 ?err=）。
//
// 带了 customerId 就回详情页：运营是在某个客户的页面上点的按钮，
// 把他弹回未筛选的列表第一页，等于让他重新找一遍那个客户。
func customerRedirect(c *gin.Context, okText, errText string) {
	q := url.Values{}
	// 回跳要保留的：页面位置（customerId 决定回哪个页面）与筛选条件。
	for _, key := range []string{"customerId", "keyword", "status", "emailVerified", "locked",
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
