package userhttp

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
)

// customer_view.go - 客户管理页的视图构造（列表/详情数据、行视图、状态选项与标签）。

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
		"title":    customerPageTitle,
		"menu":     "customers",
		"Rows":     rows,
		"Total":    customerTotal(list),
		"Page":     page,
		"Limit":    limit,
		"Counters": counters,
		// 计数徽章就是状态 / 邮箱 / 锁定三个维度的**筛选控件**（可点击链接，信息与操作合一）：
		// 同一维度不再另配下拉 —— 两套控件表达同一维度时，用户无法确定它们是否等价。
		"CounterTabs":       customerCounterTabs(counters, filter),
		"FilterActive":      customerFilterActive(filter),
		"FilterLocked":      filter.Locked,
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
		// 动词走词条（key + 中文兜底），详情页的按钮由 {{动词}}{{这个账号}} 拼成 ——
		// 两段都取值当前语言，中英界面各成句，不会混排。
		data["StatusActionKey"] = row["StatusActionKey"]
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
//
// PendingHint / LockHint 现在**只服务详情页**（admin/user/customer_detail.html 用它们做
// 状态说明）：列表页那两行整行 colspan 说明已撤掉，状态解释改由状态列表头的 .help 承载
// （词条 admin.customers.status.help.pending / .locked / .failures，模板兜底同义）——
// 逐行插入时同一句话会随行数重复，把表格切成一段段正文（02-J §2.1）。
// 列表模板因此不再读这两个字段；它们留着是因为详情页还在读，删掉会让详情页的状态说明消失。
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
		//
		// 动词本身走词条（StatusActionKey + 中文兜底）：这里原先是硬编码中文「停用 / 启用」，
		// 英文界面上与 i18n 的后缀拼起来就是「Disable这个账号」式中英混排。
		"Actionable": false,
	}
	switch item.Status {
	case customerStatusActive:
		row["Actionable"] = true
		row["NextStatus"] = customerStatusDisabled
		row["StatusActionKey"] = customerActionDisable
		row["StatusActionLabel"] = "停用"
	case customerStatusDisabled:
		row["Actionable"] = true
		row["NextStatus"] = customerStatusActive
		row["StatusActionKey"] = customerActionEnable
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

// customerCounterTabs 页头计数徽章 → 可点击的筛选链接（信息与操作合一）。
//
// 为什么徽章与下拉不能并存（admin-ui-logic §7「同一维度只给一种筛选控件」）：
// 两套控件表达同一个维度时，用户无法确定它们是否等价 —— 要么不敢点，
// 要么点了发现结果对不上，最后两组控件都失去可信度。带计数的徽章胜出：
// 它把「有多少个」与「看哪些」合并成了一个动作。
//
// URL 一律在这里生成（模板不拼查询串）：只动自己那个维度的参数，其余条件原样保留；
// 「全部」清掉状态 / 邮箱 / 锁定三个维度的取值，回到「不按维度筛」（关键词与时间仍在）。
// 计数为 0 的徽章同样可点 —— 点进去看到空列表是合理预期，禁用反而像功能坏了。
func customerCounterTabs(counters userdto.CustomerCounters, filter customerFilter) []gin.H {
	setStatus := func(v int) func(url.Values) {
		return func(q url.Values) {
			if v == customerStatusAll {
				q.Del("status")
				return
			}
			q.Set("status", strconv.Itoa(v))
		}
	}
	setVerified := func(v int) func(url.Values) {
		return func(q url.Values) {
			if v == userdto.EmailVerifiedAll {
				q.Del("emailVerified")
				return
			}
			q.Set("emailVerified", strconv.Itoa(v))
		}
	}
	tabs := []struct {
		Key      string
		LabelKey string
		Label    string
		Count    int64
		Badge    string
		Active   bool
		Apply    func(url.Values)
	}{
		{"all", "admin.customers.badge.all", "全部", counters.Total, "badge-mute",
			filter.Status == customerStatusAll && filter.EmailVerified == userdto.EmailVerifiedAll && !filter.Locked,
			func(q url.Values) { q.Del("status"); q.Del("emailVerified"); q.Del("locked") }},
		{"active", "admin.customers.badge.active", "正常", counters.Active, "badge-success",
			filter.Status == customerStatusActive, setStatus(customerStatusActive)},
		{"disabled", "admin.customers.badge.disabled", "已停用", counters.Disabled, "badge-danger",
			filter.Status == customerStatusDisabled, setStatus(customerStatusDisabled)},
		{"pending", "admin.customers.badge.pending", "待激活", counters.Pending, "badge-warning",
			filter.Status == customerStatusPending, setStatus(customerStatusPending)},
		{"locked", "admin.customers.badge.locked", "已锁定", counters.Locked, "badge-info",
			filter.Locked, func(q url.Values) { q.Set("locked", "1") }},
		{"verified", "admin.customers.badge.verified", "邮箱已验证", counters.Verified, "badge-mute",
			filter.EmailVerified == userdto.EmailVerifiedYes, setVerified(userdto.EmailVerifiedYes)},
		{"unverified", "admin.customers.badge.unverified", "邮箱未验证", counters.Unverified, "badge-mute",
			filter.EmailVerified == userdto.EmailVerifiedNo, setVerified(userdto.EmailVerifiedNo)},
	}
	out := make([]gin.H, 0, len(tabs))
	for _, t := range tabs {
		q := url.Values{}
		for k, val := range customerFilterValues(filter) {
			if val != "" {
				q.Set(k, val)
			}
		}
		t.Apply(q)
		target := customerListPath
		if len(q) > 0 {
			target += "?" + q.Encode()
		}
		out = append(out, gin.H{
			"Key": t.Key, "LabelKey": t.LabelKey, "Label": t.Label,
			"Count": t.Count, "Badge": t.Badge, "Active": t.Active, "URL": target,
		})
	}
	return out
}

// customerFilterActive 当前是否带着筛选条件（空态文案据此给出不同的下一步：
// 「筛太窄了」与「一个账号都还没有」是两件事，用一句话兜住会让运营白等）。
func customerFilterActive(filter customerFilter) bool {
	return strings.TrimSpace(filter.Keyword) != "" ||
		filter.Status != customerStatusAll ||
		filter.EmailVerified != userdto.EmailVerifiedAll ||
		filter.Locked ||
		strings.TrimSpace(filter.RegisteredFrom) != "" ||
		strings.TrimSpace(filter.RegisteredTo) != ""
}

// customerFilterValues 列表页链接要保留的筛选条件（空值由 shell.FilterBaseURL 丢弃）。
func customerFilterValues(filter customerFilter) map[string]string {
	return map[string]string{
		"keyword":        filter.Keyword,
		"status":         customerStatusQueryValue(filter.Status),
		"emailVerified":  customerVerifiedQueryValue(filter.EmailVerified),
		"locked":         customerLockedQueryValue(filter.Locked),
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

// customerLockedQueryValue 锁定筛选 → 查询参数值（不筛就不写进 URL）。
func customerLockedQueryValue(locked bool) string {
	if !locked {
		return ""
	}
	return "1"
}

// customerVerifiedQueryValue 邮箱验证 → 查询参数值（0 = 全部，不写进 URL）。
func customerVerifiedQueryValue(v int) string {
	if v == userdto.EmailVerifiedAll {
		return ""
	}
	return strconv.Itoa(v)
}

// —— 表单与文案工具 ——

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
