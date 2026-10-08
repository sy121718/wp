package usermcp

// 为什么补这一对：用户问「张三是不是老客户」「谁这周注册的」「有多少人邮箱还没验证」
// 时，答案全在 users 表里，而在此之前 AI 侧看不到任何客户数据 —— 它会说
// 「我没有查客户的工具」或者干脆编一个名字。底层一直是齐的：service 的
// ListCustomers 关键词同时匹配 邮箱 / 用户名 / 昵称 / 展示名，还带状态、
// 邮箱验证、注册时间窗与「只看已锁定」四个筛选。
//
// 只读：依赖收窄到 CustomerQueryReader（两个方法），手里没有 SetCustomerStatus ——
// 「AI 顺手把这人停用了」不会在某次改动里悄悄变得可能。
//
// 客户事实与订单事实**不在这里 join**：用户模块不解释「订单状态怎么算消费」，
// 要订单就用 order_find（它本来就能按客户名 / 邮箱反查），两边各自给全场。

// 依赖收窄到 usercontract.CustomerWriter（两个方法），拿不到改邮箱 / 改密码 /
// 改角色的能力 —— 那些确实不该由模型代劳。
//
// 边界写在工具说明里而不是靠自觉：
//   · 停用是不可逆地影响**这个人当下能不能登录**，描述里要求先 customer_get 核对身份、
//     再把「谁、现在什么状态、要改成什么」念给用户确认；
//   · 停用是可恢复的（再启用即可）且不删数据，所以给它留了通道；
//     真删账号这类动作没有对应工具。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go_wp/internal/mcp"
	"go_wp/internal/module/user/contract"
	"go_wp/internal/module/user/dto"
	"go_wp/internal/module/user/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/utils"
)

// customerFindDefaultLimit 一次最多给模型看几条客户。
//
// 工具不是分页页面：模型看到 10 条的摘要就能回答，看到 100 条只会把上下文塞满
// 然后挑错重点。上限 50 与 order_find 对齐。
const (
	customerFindDefaultLimit = 10
	customerFindMaxLimit     = 50
)

// QueryTools 返回客户模块的「按线索查客户」工具集。
func QueryTools(query usercontract.CustomerQueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("usermcp: 客户查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{customerFind(query), customerGet(query)}, nil
}

// customerFindArgs customer_find 的入参。
type customerFindArgs struct {
	Keyword        string `json:"keyword"`
	Status         string `json:"status"`
	EmailVerified  string `json:"emailVerified"`
	RegisteredFrom string `json:"registeredFrom"`
	RegisteredTo   string `json:"registeredTo"`
	LockedOnly     bool   `json:"lockedOnly"`
	Limit          int    `json:"limit"`
}

func customerFind(query usercontract.CustomerQueryReader) mcp.Tool {
	return mcp.New("customer_find", "按线索查客户",
		"按模糊线索查客户账号，用于回答「张三是不是注册过」「这周新注册了谁」「有多少人邮箱还没验证」「谁被锁定了」。\n"+
			"keyword 会同时匹配**邮箱 / 用户名 / 昵称 / 展示名**四者，用户给昵称、邮箱前缀或姓名都能命中，不必先猜是哪种。\n"+
			"不带任何筛选就返回最近注册的一批。结果里每位客户都带注册时间、最后登录时间与 IP 归属地、是否已锁定 —— "+
			"要某一位的完整资料用 customer_get。\n"+
			"要订单维度的事实（买过什么、花了多少）用 order_find 按同一关键词反查，客户工具里没有订单数据。",
		permission.UserCustomerList,
		mcp.Object("按线索查客户参数", map[string]mcp.Schema{
			"keyword": mcp.String("线索：邮箱 / 用户名 / 昵称 / 展示名，任一命中即算（可选）"),
			"status": mcp.Enum("只看某个账号状态（可选；不传则全部状态）",
				"all", "active", "pending", "disabled"),
			"emailVerified": mcp.Enum("按邮箱是否已验证筛（可选；不传则不筛）",
				"all", "yes", "no"),
			"registeredFrom": mcp.String("注册起始日，格式 YYYY-MM-DD，含当天（可选，与 registeredTo 成对使用）"),
			"registeredTo":   mcp.String("注册结束日，格式 YYYY-MM-DD，含当天（可选，与 registeredFrom 成对使用）"),
			"lockedOnly":     mcp.Boolean("只取此刻处于登录锁定的账号（可选）"),
			"limit":          mcp.Integer("最多返回几位，默认 10，上限 50"),
		}, []string{}...),
		func(ctx context.Context, args customerFindArgs) (mcp.Result, error) {
			from, err := parseDayStart(args.RegisteredFrom)
			if err != nil {
				return mcp.Result{}, fmt.Errorf("registeredFrom 不是合法日期（要 YYYY-MM-DD）：%w", err)
			}
			to, err := parseDayEnd(args.RegisteredTo)
			if err != nil {
				return mcp.Result{}, fmt.Errorf("registeredTo 不是合法日期（要 YYYY-MM-DD）：%w", err)
			}
			res, err := query.ListCustomers(ctx, &userdto.CustomerListReq{
				Keyword:        args.Keyword,
				Status:         customerStatusValue(args.Status),
				EmailVerified:  emailVerifiedValue(args.EmailVerified),
				RegisteredFrom: from,
				RegisteredTo:   to,
				LockedOnly:     args.LockedOnly,
				Limit:          clampCustomerLimit(args.Limit),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: findCustomersText(res), Data: res}, nil
		})
}

// customerGetArgs customer_get 的入参。
type customerGetArgs struct {
	CustomerID uint64 `json:"customerId"`
}

func customerGet(query usercontract.CustomerQueryReader) mcp.Tool {
	return mcp.New("customer_get", "读单个客户的资料",
		"按客户 id 读出一位客户的完整资料与登录事实（注册与最后登录的时间 / IP / 归属地、邮箱验证、账号状态、登录失败次数、当前是否锁定）。\n"+
			"id 从 customer_find 的结果里拿。不要凭姓名猜 id —— 重名与昵称重复都很常见。",
		permission.UserCustomerDetail,
		mcp.Object("读单个客户参数", map[string]mcp.Schema{
			"customerId": mcp.Integer("客户 id（customer_find 结果里的 id）"),
		}, "customerId"),
		func(ctx context.Context, args customerGetArgs) (mcp.Result, error) {
			if args.CustomerID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "customerId 必填，且要来自 customer_find 的结果"}
			}
			res, err := query.GetCustomer(ctx, args.CustomerID)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: describeCustomer(res), Data: res}, nil
		})
}

// customerStatusValue 工具入参的字符串 → dto 的取值。
//
// 空串与 "all" 都给「不过滤」：模型经常会为了显式而不传、或者传一个字面量 all，
// 两种都不该被当成「状态 = 已停用」（dto 的 0 恰好是那个意思，直接透传就会犯这个错）。
func customerStatusValue(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "active":
		return userenums.StatusActive
	case "pending":
		return userenums.StatusPending
	case "disabled":
		return userenums.StatusDisabled
	default:
		return userdto.CustomerStatusAll
	}
}

// emailVerifiedValue 同上：dto 的零值就是「不过滤」，所以只能靠显式分支表达三态。
func emailVerifiedValue(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes":
		return userdto.EmailVerifiedYes
	case "no":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

func clampCustomerLimit(n int) int {
	if n <= 0 {
		return customerFindDefaultLimit
	}
	if n > customerFindMaxLimit {
		return customerFindMaxLimit
	}
	return n
}

// parseDayStart 「YYYY-MM-DD」→ 当日零点。空串给 nil（= 该端不限）。
func parseDayStart(s string) (*utils.JSONTime, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	jt := utils.NewJSONTime(t)
	return &jt, nil
}

// parseDayEnd 「YYYY-MM-DD」→ 当日最后一秒。
//
// 不能直接用当日零点当上界：RegisteredTo 在 service 与 model 里都是**闭区间**（<=），
// 传零点会把「结束那天注册的人」整批漏掉，而结果看起来完全正常。
func parseDayEnd(s string) (*utils.JSONTime, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	jt := utils.NewJSONTime(t.Add(24*time.Hour - time.Second))
	return &jt, nil
}

// findCustomersText 把客户列表写成模型能直接引用的几句。
//
// 每行都给 **id**：它是接着调 customer_get 的唯一钥匙，只给昵称时模型要么再猜一次，
// 要么反问用户「你说是哪一位」—— 而用户刚给的线索就在这句话里。
func findCustomersText(res *userdto.CustomerListResp) string {
	if res == nil {
		return "没有查到客户。"
	}
	var b strings.Builder
	if len(res.List) == 0 {
		fmt.Fprintf(&b, "没有符合条件的客户。%s", countersText(res.Counters))
		return b.String()
	}
	fmt.Fprintf(&b, "找到 %d 位（共 %d 位符合条件）：\n", len(res.List), res.Total)
	for i, c := range res.List {
		fmt.Fprintf(&b, "%d. id=%d %s %s %s，注册 %s，最后登录 %s%s\n",
			i+1, c.ID, customerName(c), customerAccount(c), customerStatusOf(c),
			emptyAsDash(c.RegisteredAtText), lastLoginText(c), lockedSuffix(c))
	}
	fmt.Fprintf(&b, "%s", countersText(res.Counters))
	return strings.TrimRight(b.String(), "\n")
}

// describeCustomer 单个客户的完整资料。
func describeCustomer(c *userdto.CustomerResp) string {
	if c == nil {
		return "没有这位客户。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s（id=%d）\n", customerName(c), c.ID)
	fmt.Fprintf(&b, "* 账号：%s\n", customerAccount(c))
	fmt.Fprintf(&b, "* 状态：%s；邮箱%s\n", customerStatusOf(c), verifiedText(c.EmailVerified))
	fmt.Fprintf(&b, "* 注册：%s%s%s\n", emptyAsDash(c.RegisteredAtText),
		ipSuffix(c.RegisterIP), locationSuffix(c.RegisterLocation))
	fmt.Fprintf(&b, "* 最后登录：%s%s%s\n", emptyAsDash(c.LastLoginTimeText),
		ipSuffix(c.LastLoginIP), locationSuffix(c.LastLoginLocation))
	fmt.Fprintf(&b, "* 登录失败次数：%d\n", c.LoginFailureCount)
	if c.Locked {
		fmt.Fprintf(&b, "* 当前状态：**已锁定**（到 %s）\n", emptyAsDash(c.LockedUntilText))
	}
	return strings.TrimRight(b.String(), "\n")
}

// countersText 账号分布计数。
//
// 不受本次筛选影响 —— 它回答的是「这个站一共多少账号、各是什么状态」，
// 而模型最常问的正是这一类（「有多少人还没验证邮箱」），只给列表是答不出来的。
func countersText(c userdto.CustomerCounters) string {
	if c.Total == 0 {
		return ""
	}
	return fmt.Sprintf("【账号分布】全部 %d · 正常 %d · 待激活 %d · 已停用 %d · 已锁定 %d · 邮箱已验证 %d · 未验证 %d",
		c.Total, c.Active, c.Pending, c.Disabled, c.Locked, c.Verified, c.Unverified)
}

// customerName 优先展示名，退回昵称、再退回用户名 —— 展示名是空的账号很常见。
func customerName(c *userdto.CustomerResp) string {
	return firstNonEmpty(c.DisplayName, c.Nickname, c.Username, c.Email)
}

// customerAccount 账号标识：用户名 + 邮箱，两者相同就只写一个。
func customerAccount(c *userdto.CustomerResp) string {
	if c.Username == "" {
		return c.Email
	}
	if c.Email == "" || c.Email == c.Username {
		return c.Username
	}
	return c.Username + " / " + c.Email
}

// customerStatusOf 状态的展示名。
//
// 走 userenums 的映射而不是自己写一份中文表：那张表是状态文案的唯一真源
// （后台页与 /api/customer/* 都从它取），各写一份的结果是改一处、另一处静默留在旧说法上。
// 这里取中文兜底：工具结果最终要进模型上下文，而模型接的是中文提问。
func customerStatusOf(c *userdto.CustomerResp) string {
	if c.StatusLabel != "" {
		return c.StatusLabel
	}
	_, fallback := userenums.StatusLabel(c.Status)
	return fallback
}

func verifiedText(verified bool) string {
	if verified {
		return "已验证"
	}
	return "未验证"
}

func lastLoginText(c *userdto.CustomerResp) string {
	if strings.TrimSpace(c.LastLoginTimeText) == "" {
		// 「从没登录过」与「登录时间字段没取到」是两件事：前者是待激活账号的常态，
		// 显示成空或「-」会让模型以为数据缺失并去猜。
		return "从未登录"
	}
	return c.LastLoginTimeText + locationSuffix(c.LastLoginLocation)
}

func lockedSuffix(c *userdto.CustomerResp) string {
	if !c.Locked {
		return ""
	}
	return "（已锁定）"
}

func ipSuffix(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return ""
	}
	return "，IP " + ip
}

func locationSuffix(loc string) string {
	if strings.TrimSpace(loc) == "" {
		return ""
	}
	return "（" + loc + "）"
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// WriteTools 返回客户模块的写工具集。
func WriteTools(w usercontract.CustomerWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("usermcp: 客户写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{customerSetStatus(w), customerUnlock(w)}, nil
}

// 状态取值：与 enums 的常量一一对应，不另造一套说法。
const (
	customerStatusActiveArg   = "active"
	customerStatusDisabledArg = "disabled"
)

type customerSetStatusArgs struct {
	CustomerID uint64 `json:"customerId"`
	Status     string `json:"status"`
}

func customerSetStatus(w usercontract.CustomerWriter) mcp.Tool {
	return mcp.NewWrite("customer_set_status", "启用 / 停用客户账号",
		"把某个客户账号设为正常（active）或停用（disabled）。\n"+
			"停用会让他**立刻登不上**，但数据全部保留，再启用即可恢复。\n"+
			"**执行前必须先做两件事**：① 用 customer_get 确认这个 id 到底是谁（核对邮箱或昵称）；"+
			"② 把「谁、从什么状态改成什么」念给用户确认。别在用户只说了「把那个刷单的停了」时自己挑一个 id 猜。\n"+
			"需要 customerId 时用 customer_find 按邮箱 / 昵称搜。",
		permission.UserCustomerStatus,
		mcp.Object("客户账号状态参数", map[string]mcp.Schema{
			"customerId": mcp.Integer("客户 id（用 customer_find / customer_get 拿到的 id）"),
			"status": mcp.Enum("目标状态：active=正常（可登录），disabled=停用（登不上，数据保留）",
				customerStatusActiveArg, customerStatusDisabledArg),
		}, "customerId", "status"),
		nil,
		func(ctx context.Context, args customerSetStatusArgs) (mcp.Result, error) {
			status, ok := customerStatusValueOf(args.Status)
			if !ok {
				return mcp.Result{}, &mcp.ArgsError{Msg: "status 只能是 active 或 disabled"}
			}
			if args.CustomerID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "customerId 必填：先用 customer_find 按邮箱或昵称找到人"}
			}
			res, err := w.SetCustomerStatus(ctx, &userdto.CustomerStatusReq{
				CustomerID: args.CustomerID,
				Status:     status,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: statusChangeText(res)}, nil
		})
}

// customerStatusValueOf 工具层的说法 → 落库取值。
//
// 不用「空串当默认」：这里的默认值含义截然不同（0 是**停用**），
// 把没给的参数当 0 处理会把一次手滑变成一次停用。
func customerStatusValueOf(v string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case customerStatusActiveArg:
		return userenums.StatusActive, true
	case customerStatusDisabledArg:
		return userenums.StatusDisabled, true
	default:
		return 0, false
	}
}

// statusChangeText 状态变更回执。
//
// 停用要说清「他立刻登不上」，并给出恢复办法 —— 用户按下这个按钮之后最常见的
// 下一个问题是「我刚才是不是弄错了」，回答里没有恢复路径他就得自己找。
func statusChangeText(res *userdto.CustomerStatusResp) string {
	label := res.StatusLabel
	if strings.TrimSpace(label) == "" {
		if _, fallback := userenums.StatusLabel(res.Status); strings.TrimSpace(fallback) != "" {
			label = fallback
		} else {
			label = fmt.Sprint(res.Status)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "客户 %d 已置为「%s」。", res.CustomerID, label)
	if res.Status == userenums.StatusDisabled {
		b.WriteString("\n他现在登不上；数据都在，需要时把 status 设回 active 即可恢复。")
	} else {
		b.WriteString("\n他现在可以正常登录。")
	}
	return b.String()
}

type customerUnlockArgs struct {
	CustomerID uint64 `json:"customerId"`
}

func customerUnlock(w usercontract.CustomerWriter) mcp.Tool {
	return mcp.NewWrite("customer_unlock", "解除客户登录锁定",
		"清掉某个客户的登录锁定与失败次数（连续输错密码会被自动锁定，这个工具就是运营侧的解封）。\n"+
			"与 customer_set_status 的区别：**锁定是按时间自动解除的临时状态，停用是管理动作**。"+
			"被锁的账号本来就会自己恢复，只是要等；停用不会自己恢复。用户说「登不上」时先看清是哪一种（customer_get 里有）。\n"+
			"如果账号本来就没锁，工具会如实告诉你「本来就没锁」—— 那不是失败。",
		permission.UserCustomerUnlock,
		mcp.Object("解除登录锁定参数", map[string]mcp.Schema{
			"customerId": mcp.Integer("客户 id（用 customer_find / customer_get 拿到的 id）"),
		}, "customerId"),
		nil,
		func(ctx context.Context, args customerUnlockArgs) (mcp.Result, error) {
			if args.CustomerID == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "customerId 必填：先用 customer_find 按邮箱或昵称找到人"}
			}
			res, err := w.UnlockCustomer(ctx, &userdto.CustomerUnlockReq{CustomerID: args.CustomerID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: unlockText(res)}, nil
		})
}

// unlockText 解锁回执。
//
// Unlocked 与 Cleared 是两件事，分开说：运营点了个不会变化的按钮时需要知道的
// 是「它本来就没锁」，而不是又一次看到「已解除」然后下次再来点一遍。
func unlockText(res *userdto.CustomerUnlockResp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "客户 %d：", res.CustomerID)
	switch {
	case res.Unlocked && res.Cleared:
		b.WriteString("已解除登录锁定，并清空了积攒的登录失败次数。")
	case res.Unlocked:
		b.WriteString("已解除登录锁定。")
	case res.Cleared:
		b.WriteString("当时并没有处于锁定状态，只清空了残留的登录失败次数（这些计数会让它更容易被再次锁上）。")
	default:
		b.WriteString("本来就没有锁定，也没有失败计数 —— 没有做任何改动。")
	}
	return b.String()
}
