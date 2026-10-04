package userhttp

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/web/shell"
)

// customer_view.go - 客户管理页的视图构造（列表/详情数据、行视图、状态选项与标签）。

// customerListPageData 组装列表页渲染数据（纯函数：不取数、不依赖 gin.Context 之外的东西）。
//
// 抽出来的理由同订单页：渲染键名与计数口径只在这里定义一次，
// 真实渲染测试可以直接喂数据走同一条组装路径，不必在测试里手抄一份键名
// （手抄的那份会随模板演进静默失配，而那正是「页面上少了一块、断言却通过」的成因）。
func customerListPageData(tr func(key, fallback string) string, list *userdto.CustomerListResp, filter customerFilter,
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
		"title":    userLabelOf(tr, customerPageTitleLabel),
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
	selected string, summary *ordercontract.CustomerOrderSummaryResp,
	projectsFailed, summaryFailed bool, pageErr, pageOk string, c *gin.Context) gin.H {
	data := gin.H{
		"title":           userLabelOf(shell.TranslateFor(c), customerDetailPageTitleLabel),
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
	// 会员等级那一块的键**恒设零值**（BIZ-3）：调用方（handle）随后用
	// applyCustomerMembership 覆盖成真实值。放在组装函数里而不是让调用方补，
	// 是因为这里出去的 data 有 6 个消费点（含渲染测试直接调本函数）——
	// 漏一处就是模板缺键 → 渲染中断 → 整页 500（Jet 的判据见 internal/templates/CLAUDE.md），
	// 而那 4 处测试路径恰恰不该关心会员那块的实现。
	applyCustomerMembership(data, customerMembershipView{})

	if detail != nil {
		row := customerRow(detail)
		data["Customer"] = row
		data["ID"] = detail.ID
		data["Username"] = detail.Username
		data["Email"] = detail.Email
		data["DisplayLabel"] = row["DisplayLabel"]
		// 状态与邮箱验证的展示名一律「key + 中文兜底」两个键（来源 userenums），
		// 模板取词（tr(key, fallback)）——给中文值就等于英文界面永远中文。
		data["StatusLabelKey"] = row["StatusLabelKey"]
		data["StatusLabelFallback"] = row["StatusLabelFallback"]
		data["StatusBadge"] = row["StatusBadge"]
		data["EmailVerified"] = detail.EmailVerified
		data["EmailVerifiedKey"] = row["EmailVerifiedKey"]
		data["EmailVerifiedFallback"] = row["EmailVerifiedFallback"]
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
		data["NextStatus"] = row["NextStatus"]
		// 状态说明同样是「key + 中文兜底」（键存在 = 这个状态有说明要讲）：
		// 直接渲染中文等于英文界面永远中文，而这两句正是详情页上唯一的解释。
		data["PendingHintKey"] = row["PendingHintKey"]
		data["PendingHintFallback"] = row["PendingHintFallback"]
		// 动词走词条（key + 中文兜底），详情页的按钮由 {{动词}}{{这个账号}} 拼成 ——
		// 两段都取值当前语言，中英界面各成句，不会混排。
		data["StatusActionKey"] = row["StatusActionKey"]
		data["StatusActionLabel"] = row["StatusActionLabel"]
		// 锁定提示分开给：锁定的账号「登不上去」但状态是正常的，
		// 这两件事在页面上必须能分辨（否则运营会去点停用）。
		data["LockHintKey"] = row["LockHintKey"]
		data["LockHintFallback"] = row["LockHintFallback"]
	}
	if summary != nil {
		data["OrderCount"] = summary.OrderCount
		data["PaidOrderCount"] = summary.PaidOrderCount
		data["TotalAmountLabel"] = summary.TotalAmountLabel
		data["LastOrderNo"] = summary.LastOrderNo
		// 原样传：模板靠它是否为空来区分「有最近一单」与「还没下过单」，
		// 在这里转成「—」会让两种状态长得一模一样（于是零订单显示成一行破折号）。
		data["LastOrderTimeText"] = summary.LastOrderTimeText
		// 最近一单的状态同样走「key + 中文兜底」，且**与订单页共用同一份映射**：
		// 真源在 order 模块（orderenums.OrderStatusLabel），跨模块经 ordercontract 引用 ——
		// 这里原先自留一份 userenums.OrderStatusLabel，改一处另一处会静默漂移。
		data["LastOrderStatusKey"], data["LastOrderStatusFallback"] = ordercontract.OrderStatusLabel(summary.LastOrderStatus)
		// 最近一单的直达链接：订单页按 orderId 参数展开详情（不新开路由）。
		data["LastOrderURL"] = customerOrderURL(selected, summary.LastOrderID)
	} else {
		data["HasSummary"] = false
	}
	return data
}

// customerRow 一行客户（列表与详情共用同一份事实；两种页面看到的数字因此不可能不一致）。
//
// PendingHintKey / LockHintKey（+ 各自的中文兜底）现在**只服务详情页**
// （admin/user/customer_detail.html 用它们做状态说明）：列表页那两行整行 colspan 说明已撤掉，
// 状态解释改由状态列表头的 .help 承载 —— 逐行插入时同一句话会随行数重复，
// 把表格切成一段段正文（02-J §2.1）。两者用的是**同一批词条**
// （userenums.LabelKeyPendingHint / LockedHint / FailuresHint），所以两个页面上的解释不会漂移；
// 列表模板读词条、详情页读这里的键 —— 同一句话，一处取词两处显示。
func customerRow(item *userdto.CustomerResp) gin.H {
	// 展示名一律「key + 中文兜底」两个键，且唯一的来源是 userenums ——
	// 页面不再自造文案（此前这里给的是中文，模板直接渲染，英文界面恒中文）。
	statusKey, statusFallback := userenums.StatusLabel(item.Status)
	verifyKey, verifyFallback := userenums.EmailVerifiedLabel(item.EmailVerified)
	row := gin.H{
		"ID":                    item.ID,
		"Username":              customerTextOrEmpty(item.Username),
		"Email":                 customerTextOrEmpty(item.Email),
		"DisplayLabel":          customerDisplayLabel(item),
		"Status":                item.Status,
		"StatusLabelKey":        statusKey,
		"StatusLabelFallback":   statusFallback,
		"StatusBadge":           customerStatusBadge(item.Status),
		"EmailVerified":         item.EmailVerified,
		"EmailVerifiedKey":      verifyKey,
		"EmailVerifiedFallback": verifyFallback,
		"RegisteredAtText":      customerTextOrEmpty(item.RegisteredAtText),
		"RegisterIP":            customerTextOrEmpty(item.RegisterIP),
		"RegisterLocation":      customerTextOrEmpty(item.RegisterLocation),
		"LastLoginTimeText":     customerTextOrEmpty(item.LastLoginTimeText),
		"LastLoginIP":           customerTextOrEmpty(item.LastLoginIP),
		"LastLoginLocation":     customerTextOrEmpty(item.LastLoginLocation),
		"Locked":                item.Locked,
		"LockedUntilText":       customerTextOrEmpty(item.LockedUntilText),
		"LoginFailureCount":     item.LoginFailureCount,
		"DetailURL":             customerDetailURL(item.ID),
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
		// 说明文案与列表页表头 .help 共用同一条词条（userenums.LabelKeyPendingHint）：
		// 同一句解释在两处各写一份，改一处另一处会静默留在旧说法上。
		row["PendingHintKey"] = userenums.LabelKeyPendingHint
		row["PendingHintFallback"] = userenums.LabelPendingHint
	}
	if item.Locked {
		row["LockHintKey"] = userenums.LabelKeyLockedHint
		row["LockHintFallback"] = userenums.LabelLockedHint
	} else if item.LoginFailureCount > 0 {
		row["LockHintKey"] = userenums.LabelKeyFailuresHint
		row["LockHintFallback"] = userenums.LabelFailuresHint
	}
	return row
}

// customerStatusText 状态取值 → **当前语言**的展示名（按状态值取词，不拿别处给的中文反查）。
//
// 本模块的两个出口共用同一份映射（userenums.StatusLabel）：后台页 customerRow 给模板
// (key, 兜底) 由模板 tr 取词；/api/customer/* 的 handler 拿不到模板，直接要已取词的字符串。
// 认不出的取值走 userenums 的「未知状态(N)」兜底 —— 它在 key 为空串时由取词函数原样返回。
func customerStatusText(tr func(key, fallback string) string, status int) string {
	key, fallback := userenums.StatusLabel(status)
	return userLabelOf(tr, userLabel{key: key, fallback: fallback})
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
	// 徽章文案与行内状态标签共用**同一份来源**（userenums）：此前 tabs 自带一套 key、
	// 行内没有任何 key，同一个语义在两处各说各话，改一处另一处静默不动。
	allKey, allLabel := userenums.StatusLabel(customerStatusAll)
	activeKey, activeLabel := userenums.StatusLabel(customerStatusActive)
	disabledKey, disabledLabel := userenums.StatusLabel(customerStatusDisabled)
	pendingKey, pendingLabel := userenums.StatusLabel(customerStatusPending)
	lockedKey, lockedLabel := userenums.LabelKeyStatusLocked, userenums.LabelStatusLocked
	verifiedKey, verifiedLabel := userenums.EmailVerifiedLabel(true)
	unverifiedKey, unverifiedLabel := userenums.EmailVerifiedLabel(false)
	tabs := []struct {
		Key      string
		LabelKey string
		Label    string
		Count    int64
		Badge    string
		Active   bool
		Apply    func(url.Values)
	}{
		{"all", allKey, allLabel, counters.Total, "badge-mute",
			filter.Status == customerStatusAll && filter.EmailVerified == userdto.EmailVerifiedAll && !filter.Locked,
			func(q url.Values) { q.Del("status"); q.Del("emailVerified"); q.Del("locked") }},
		{"active", activeKey, activeLabel, counters.Active, "badge-success",
			filter.Status == customerStatusActive, setStatus(customerStatusActive)},
		{"disabled", disabledKey, disabledLabel, counters.Disabled, "badge-danger",
			filter.Status == customerStatusDisabled, setStatus(customerStatusDisabled)},
		{"pending", pendingKey, pendingLabel, counters.Pending, "badge-warning",
			filter.Status == customerStatusPending, setStatus(customerStatusPending)},
		{"locked", lockedKey, lockedLabel, counters.Locked, "badge-info",
			filter.Locked, func(q url.Values) { q.Set("locked", "1") }},
		{"verified", verifiedKey, verifiedLabel, counters.Verified, "badge-mute",
			filter.EmailVerified == userdto.EmailVerifiedYes, setVerified(userdto.EmailVerifiedYes)},
		{"unverified", unverifiedKey, unverifiedLabel, counters.Unverified, "badge-mute",
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
//
// **按状态值查，不按文案查**：同仓反例是 order 的 couponStatusBadge(statusLabel string)——
// 拿已翻译 / 已格式化的文案反查样式表，改一句词条就让样式静默失效（不报错、测试也不红）。
// 展示细节（类名）留在这里，不进 enums：enums 管「枚举 → 展示名」，管不了 CSS。
func customerStatusBadge(status int) string {
	switch status {
	case customerStatusActive:
		return "badge-success"
	case customerStatusDisabled:
		return "badge-danger"
	case customerStatusPending:
		return "badge-warning"
	default:
		return "badge-mute"
	}
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
