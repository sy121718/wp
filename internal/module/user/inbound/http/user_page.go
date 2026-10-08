package userhttp

// 这一页回答的是 RFM 回答不了的问题：**这段时间来的新人，之后还回不回来**。
// RFM 是横截面（此刻谁值多少），群组留存是纵向的（同一批人随时间怎么衰减）。
//
// 矩阵的形状（行 = 首单所在月，列 = 相对月序号）与每一格的取值全在订单模块
// （ordercontract.CustomerCohortReader）。本页不重算任何比例 —— 页头「共 N 个新客户」
// 必须与客户概览页的「新客」是同一个数，各算一次就会分叉。
//
// **空格的语义要在页面上说清楚**：还没到的月份是空白，不是 0%。两者混在一起时
// 最新几个月的留存率会显示成一片 0%，读起来像断崖式流失，而真实原因是时间还没到。

// 这一页只回答一个问题：**这段时间客户是怎么变的** —— 来了多少新客、多少人回来下单、
// 复购率多少。四个数出自订单模块的一条聚合（ordercontract.CustomerGrowthReader），
// 本页不做任何自己的算术（口径的解释权只在拥有 orders 表的模块）。
//
// **为什么这一页不做「自定义区间」**：自定义要一套收敛规则（日期非法、首尾颠倒、
// 终点在未来、跨度超长），概览页已经解决过一次。在这里先复制一遍的代价不是多写二十行，
// 而是**两套收敛规则迟早分叉**（一个把未来日期收成今天、另一个报错），
// 而运营看不出哪一页是对的。先只给预设；真有人要自定义时，正确做法是把它收口成
// 共享实现（`pkg/`），而不是让这一页长出自己的版本。

// 这一页回答「这些人各自值多少」：把区间内下过单的人按 R（多久没来）/ F（来了几次）/
// M（花了多少）各打 1-5 分，总分决定分段（高价值 / 潜力 / 一般）。
//
// **打分是相对的**：每一维的五分位都按当次查询的那批人算，所以页面上的分数必须
// 与「分位」两个字一起读 —— 一个人在这个区间是 R=5，换一个区间可能只有 R=3。
// 这一点写在页头说明里，而不是只留在代码注释里（否则它一定会被读成绝对等级）。
//
// 客户名来自客户模块（订单模块只交 id）：RFM 的口径在订单侧、客户的资料在客户侧，
// 页面负责把两边拼起来 —— 这也是这一页属于「客户模块的页面」而不是订单模块的原因。

// handler 的职责边界（见 internal/module/CLAUDE.md）：绑定参数 → 调 service → 输出响应。
// 这里**不做业务判断**：邮箱格式、密码强度、账号状态全部在 service 里，
// 这里只负责把表单字段搬进 dto，以及把 service 的错误搬回页面。

// 只有两个：attachUserSession（尽力解析，不阻断）与 requireUser（要求已登录）。
// 刻意不做「按权限点鉴权」的中间件：访客能做的事全是「操作自己的账号」，
// 归属校验写在 service 的 SQL 条件里（`WHERE id = ? AND user_id = ?`）比写在中间件里可靠 ——
// 中间件只能判断「登录没有」，判断不了「这条记录是不是他的」。

// Package userhttp 用户模块（访客账号）的 HTTP 接入层：注册 / 验证 / 登录 / 账号中心。
//
// # 为什么这些路由不在 /api 下
//
// /api 那组挂了 SessionAuthMiddleware + CSRFMiddleware + CasbinMiddleware 三件套，
// 它们是**管理后台**的认证体系：Casbin 的 subject 是 sys_admin.id，权限点是后台菜单的权限点。
// 访客账号没有权限点、也不该进 Casbin 的策略表（那会让「登录」这件事变成一次授权决策）。
//
// 所以访客侧自带一套**更窄**的链路：自己的 cookie 会话 + 自己的 CSRF token，
// 没有 Casbin。少一层不是省事，而是把「谁有权做什么」收敛到业务代码里 ——
// 访客能做的事只有「操作自己的账号」，这个判断不需要策略引擎。

// cookie 里只放会话令牌本身，业务字段一律不入 cookie：
// cookie 是客户端可见的（虽然签名防篡改），放进去的东西改起来麻烦、还会随每个请求来回传。

// **只加展示、不加领域**：本文件没有任何写方法 —— 不指定等级、不解锁、不重算。
// 那些是 membership 模块自己的后台页（/admin/membership/*）的职责，各带各的权限点；
// 在客户页上顺手给一个「改他的等级」按钮，等于把两个权限点合成了一个。
//
// 端口经装配期注入，形态照既有的 CustomerAdminPort / CustomerOrderSummaryReader：
//	Reader       —— 读一条会员身份（收窄的只读接口，拿不到等级 CRUD 与归属写入）；
//	FacingTexter —— 把会员模块的业务错误转成一句话（客户页拿不到它的 enums 白名单，
//	                没有这条出口就只能直出 err.Error() 或一律通用提示）。
//
// 两处「不炸页」的兜底：
//   · 端口未注入 / 没选工程 / 解析失败一律给**可见文案**，且渲染键**始终存在**
//     （Jet 的 `{{if .X}}` 遇到缺键会中断整页渲染 → 500，见 internal/templates/CLAUDE.md）；
//   · 等级名等字段用 isset 兜底，缺键不炸（渲染键恒设零值，模板侧仍有判断）。

// 这一组全部挂在 requireUser 之下：未登录一律 302 到登录页。
// userID **只从会话里取**，页面上的表单里没有、也不接受任何用户 id 字段 ——
// 从请求参数取 id 的账号接口是「谁都能改别人的资料」，而且它看起来完全正常。

// 全部走**原生表单**（无 JS 依赖）：访客侧是公开页面，必须在脚本不可用时也能完成注册与登录。
// 每个表单显式带 csrf_token 隐藏域（原生表单不会自动带 HTMX 的 hx-headers）。

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gsessions "github.com/gorilla/sessions"

	"go_wp/internal/builder"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/membership/contract"
	"go_wp/internal/module/membership/dto"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/user/contract"
	"go_wp/internal/module/user/dto"
	"go_wp/internal/module/user/enums"
	"go_wp/internal/module/user/service"
	"go_wp/internal/shell"
	"go_wp/pkg/auth"
	"go_wp/pkg/crypto"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

// customerCohortPath 页面路径（菜单 seed 与导航高亮都用它）。
const customerCohortPath = "/admin/customers/cohort"

// customerCohortTitleLabel 页标题（词条 key + 兜底文案）。
//
// 必须在 Go 侧设进 data：layout.html 的 `{{.title}}` 是**无条件读**的，
// shell.Prepare 的 injectI18n 只翻译已有非空字符串、不注入 —— 漏设的结果是
// 整页 500，而错误信息里只有模板行号，看不出是「少了这个键」。
var customerCohortTitleLabel = userLabel{"admin.customer.cohort.title", "群组留存"}

// customerCohortUnavailableLabel 端口缺席时的说明（不是「没有客户」——那是两种不同的空）。
var customerCohortUnavailableLabel = userLabel{
	"admin.customer.cohort.unavailable",
	"群组留存暂时不可用（数据没接上）。",
}

// CustomerCohortPage 群组留存（GET /admin/customers/cohort）。
func (h *customerPageHandle) CustomerCohortPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())

	// 工程：与客户概览页逐字同规则 —— 先看 ?project=，没有就用第一个；
	// 本页同样不渲染工程切换器（这一页的工程由从列表页带过来的上下文决定）。
	var projects []projectcontract.ProjectResp
	if h.projects != nil {
		if list, perr := h.projects.List(ctx); perr == nil {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var res *orderdto.CustomerCohortResp
	var errText string
	switch {
	case h.cohort == nil:
		errText = userLabelOf(tr, customerCohortUnavailableLabel)
	case selected == "":
		// 没有工程就没有订单可算 —— 「还没建站点工程」是正常状态，不是错误。
		errText = ""
	default:
		got, err := h.cohort.CustomerCohortByRange(ctx, &orderdto.CustomerCohortReq{
			ProjectID: selected,
			From:      rng.From,
			To:        rng.To,
		})
		if err != nil {
			errText = customerFacingError(c, err)
		} else {
			res = got
		}
	}

	data := customerOverviewPageData(tr, rng, nil, "")
	data["Path"] = customerCohortPath
	data["title"] = userLabelOf(tr, customerCohortTitleLabel)
	data["Err"] = errText
	data["CohortReady"] = res != nil
	// 列头在 Go 侧定（Jet 的 `{{range}}` 只能遍历集合，没有「遍历 0..N」这种写法）。
	// 列头用**相对月序号**而不是年月：每一行的首单月不同，同一列在各行是不同月份，
	// 把某一行的月份当列头会让其它行的格子对不上号。
	//
	// **三个键无论有没有数据都要设**（空切片 / 零值）：模板里会 `len()` 它们，
	// 而 Jet 对缺席的键（nil）求 len 是运行时错误 —— 症状是整页 500，
	// 而错误信息只有模板行号，看不出是「少了这个键」。colspan 同理，
	// 在 Go 侧算好（Jet 的算术是浮点，`{{len(x) + 2}}` 会渲染成「2」这种怪值）。
	cols := []gin.H{}
	rows := res
	if rows != nil {
		for k := 0; k < rows.Months; k++ {
			cols = append(cols, gin.H{"Index": k, "Text": strconv.Itoa(k)})
		}
	}
	data["Columns"] = cols
	data["Colspan"] = len(cols) + 2
	if res != nil {
		data["Cohorts"] = res.Cohorts
		data["Customers"] = res.Customers
		data["Months"] = res.Months
		data["Rows"] = res.Rows
	} else {
		data["Rows"] = []orderdto.CustomerCohortRow{}
	}
	c.HTML(200, "admin/user/customer_cohort", shell.Prepare(c, data))
}

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
//  4. 写动作的结论由 shell.RenderJump 渲染成**整页提示**（对应 ThinkPHP 的 success() /
//
//     error()）：文案走响应体、不进 URL，所以不再需要「查询参数是用户可编辑的，
//
//     不能拿它当业务提示直接显示」那套读侧白名单判定（?err= / ?ok= / ?done= 已整批删除）。

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

// 本页自造的文案（参数级提示 / 装配降级说明）。
//
// userenums.UserFacingMessages 只登记「用户模块产出的」文案；下面这几条是后台页面
// 自己造的，**直接渲染**（或经提示页渲染），不再经 ?err= 回显 —— 那条读侧通道已随
// 「写结论走 shell.RenderJump」整批删除，所以不再需要一份「受控文案集合」来证明
// 这条提示出自本仓。形态统一为 key + 中文兜底，取词走 userLabelOf。
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
	// customerStatusInvalidLabel 目标状态取值不合法（表单可伪造，这里先拦一道）。
	customerStatusInvalidLabel = userLabel{userenums.ErrCustomerStatusInvalid, "账号状态取值不合法"}
)

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
	// growth 区间客户增长（新客 / 复购 / 回头客）。
	//
	// 同样经 setter 注入（见 SetCustomerGrowth），理由与会员展示端口一样：
	// NewCustomerPageHandle 有 10+ 处直调（含大量渲染测试），为一块展示改签名会把它们全卷进来。
	// 口径与计算都在订单模块，这里只拿结论 —— 客户模块读不到 orders 表。
	growth ordercontract.CustomerGrowthReader
	// segments 客户分段取 id（列表页按「新客 / 回头客 / 复购」筛选）。
	//
	// 同样经 setter 注入：口径在订单模块（只有它看得到 orders 表），这里只拿 id 列表。
	segments ordercontract.CustomerSegmentReader
	// rfm 客户 RFM 分层（RFM 分析页）。
	rfm ordercontract.CustomerRfmReader
	// cohort 群组留存矩阵（Cohort 分析页）。
	cohort ordercontract.CustomerCohortReader
	// membershipAdmin / membershipTiers 会员等级维度的筛选（客户列表 ?tier=）。
	//
	// **与上面的 membership（Reader）是两个不同的端口**：Reader 读某一个人的等级，
	// 这两个是「按等级反查一批人」（归属列表）与「列出可选等级」（等级配置）。
	// 收窄到这两个只读接口 —— 客户列表不该有改等级 / 解锁 / 重算的能力。
	membershipAdmin membershipcontract.AssignmentAdminPort
	membershipTiers membershipcontract.TierAdminPort
}

// SetMembershipFilters 注入会员等级筛选的两个只读端口（允许为 nil：页面据此
// 渲染「按等级筛选暂时不可用」，而不是静默不筛）。
func (h *customerPageHandle) SetMembershipFilters(admin membershipcontract.AssignmentAdminPort, tiers membershipcontract.TierAdminPort) {
	h.membershipAdmin = admin
	h.membershipTiers = tiers
}

// SetCustomerCohort 注入群组留存端口（允许为 nil：分析页会明确说「暂时不可用」，
// 而不是渲染一张空矩阵 —— 空矩阵会被读成「这批人一个月都没回来」）。
func (h *customerPageHandle) SetCustomerCohort(cohort ordercontract.CustomerCohortReader) {
	h.cohort = cohort
}

// SetCustomerRfm 注入 RFM 端口（允许为 nil：分析页会明确说「暂时不可用」，
// 而不是渲染三格 0 —— 0 会被读成「这段时间一个客户都没有」）。
func (h *customerPageHandle) SetCustomerRfm(rfm ordercontract.CustomerRfmReader) {
	h.rfm = rfm
}

// SetCustomerSegments 注入客户分段端口（允许为 nil：列表页会明确说「筛不了」，
// 而不是把「没筛」显示成筛选结果）。
func (h *customerPageHandle) SetCustomerSegments(segments ordercontract.CustomerSegmentReader) {
	h.segments = segments
}

// SetCustomerGrowth 注入区间客户增长端口（允许为 nil：概览页据此渲染一句
// 「客户增长数据暂不可用」，而不是显示一片 0 —— 0 会被当成真实统计）。
func (h *customerPageHandle) SetCustomerGrowth(growth ordercontract.CustomerGrowthReader) {
	h.growth = growth
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
	// Segment 消费分段：""（全部）/ new / returning / repurchasing。
	//
	// 这一条与上面几条**不同源**：它问的是「这个人下过什么单」，只有订单模块答得出来，
	// 所以要先把 id 要回来再筛客户行（见 segmentCustomerIDs）。
	Segment string
	// SegmentFrom / SegmentTo 分段的时间窗口（YYYY-MM-DD，闭区间）。
	//
	// 只在 Segment 非空时有意义。**与注册时间筛选是两个不同的窗口**，
	// 不能合成一个日期控件 —— 「这周来的新客」与「这周注册的人」是两批人。
	SegmentFrom string
	SegmentTo   string
	// MinOrders 复购次数下限（0 = 用默认门槛）。
	//
	// 它单独存在（而不是并进 Segment）：「下过 ≥3 单的客户」本身就是完整的一句话，
	// 不选分段也该能筛。给了次数不选分段时，订单模块按「复购」处理（同一段代码）。
	MinOrders int
	// TierID 会员等级（0 = 不限）。
	//
	// 这一维问的是「会员模块怎么给这个人定的级」，与订单行为、RFM 都不相干 ——
	// 三个筛选同时给出时必须**同时**成立（见 CustomersPage 的两两求交）。
	TierID int64
	// RfmSegment RFM 分段：""（全部）/ vip / potential / low_value。
	//
	// **与 Segment 是两套不同的分段**：那三个看下单行为（新客 / 回头客 / 复购），
	// 这一条看 RFM 总分。同时给出时取交集 —— 两个条件都必须成立。
	RfmSegment string
}

// customerPageSegment 分段查询值 → 已知分段（认不出一律回落「全部」）。
//
// 与状态筛选同一条口径：URL 是用户可编辑的，一个手改出来的未知分段名不该让整页报错，
// 但也不能被当成某种分段 —— 回落「全部」是唯一不会静默说错话的选择。
// （往下传给订单模块时它还会再判一次白名单，认不出会当场拒。）
func customerPageSegment(v string) string {
	switch strings.TrimSpace(v) {
	case customerSegmentNew, customerSegmentReturning, customerSegmentRepurchasing:
		return strings.TrimSpace(v)
	}
	return ""
}

// 分段名（与订单模块的白名单同字面量；两处都有测试钉住）。
const (
	customerSegmentNew          = "new"
	customerSegmentReturning    = "returning"
	customerSegmentRepurchasing = "repurchasing"
)

// customerSegmentMinOrders 复购次数的可选档位（与订单模块的白名单同集合）。
//
// 收任意值的话，「≥ 7 次」这种档位会出现在 URL 里并被分享、被回放；不在这张表里的
// 一律回落 0（= 不按次数筛，而不是回落到默认门槛 2 —— 回落会让 URL 上写着 7 的筛选
// 实际跑的是 2，结果看起来正常却少了一半人）。
var customerSegmentMinOrders = map[int]bool{2: true, 3: true, 5: true, 10: true}

// customerRfmSegments RFM 分段的可选值（与订单模块的白名单同集合）。
var customerRfmSegments = map[string]bool{"vip": true, "potential": true, "low_value": true}

// customerPageRfmSegment RFM 分段查询值 → 已知分段（认不出一律回落「全部」）。
//
// 与消费分段同一条口径：URL 是用户可编辑的，手改出来的未知分段不该让整页报错，
// 也不能被当成某种分段 —— 回落「全部」是唯一不会静默说错话的选择。
func customerPageRfmSegment(v string) string {
	trimmed := strings.TrimSpace(v)
	if customerRfmSegments[trimmed] {
		return trimmed
	}
	return ""
}

// customerPageTierID 等级查询值 → 等级 ID（认不出或非正数一律回落 0 = 不限）。
//
// **与「等级已被删除」的情形刻意区分开**：URL 里带着一个下拉框中已不存在的 id 时，
// 这里照旧把它当筛选条件传下去（结果是零个人），而不是回落成「不限等级」。
// 后者会让页面显示全部客户 —— 用户以为筛过了，实际筛选已被静默取消。
// 下拉框此时会显示「不限等级」（浏览器找不到匹配项就选第一项），与 URL 不一致，
// 但那是「这个等级没了」的正确表现：结果为零，用户会去看原因。
//
// 这里**不能**像分段那样查白名单表：等级是运营自己建的，ID 事先不知道。
// 回落 0 而不是报错（URL 可编辑），但真正的归属判定在会员模块 ——
// 一个不存在的 id 查出来就是零个人，那是正确结果。
func customerPageTierID(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// customerPageMinOrders 次数查询值 → 已知档位（认不出一律回落 0）。
func customerPageMinOrders(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || !customerSegmentMinOrders[n] {
		return 0
	}
	return n
}

// segmentCustomerIDs 把「消费分段 + 时间窗口」换成一批客户 id。
//
// 返回值是三态（调用方据此决定「不筛 / 筛 / 报错」）：
//   - Segment 为空            → nil，不筛；
//   - 端口缺席或取数失败      → nil + 非空错误文案（调用方**不查列表**，见 CustomersPage）；
//   - 取到了（含零个人）      → **非 nil** 切片（零个人是空切片）。
//
// 第三种里的空切片是关键：它一路传到 model 的 `id IN (...)` 之前被短路成空结果。
// 若在这里折成 nil，「这个分段一个人都没有」就会显示成「全部客户」。
//
// 工程取第一个（与客户概览页同规则）：客户列表页没有工程选择器，
// 而分段口径是工程维度的 —— 不选工程就没法算。
func (h *customerPageHandle) segmentCustomerIDs(ctx context.Context, f customerFilter) ([]int64, string) {
	if f.Segment == "" && f.MinOrders == 0 {
		return nil, ""
	}
	if h.segments == nil {
		// 没接线时明确说「筛不了」，而不是当作没筛。
		return nil, customerSegmentUnavailableText
	}
	if h.projects == nil {
		return nil, customerSegmentUnavailableText
	}
	list, perr := h.projects.List(ctx)
	if perr != nil {
		return nil, customerSegmentUnavailableText
	}
	if len(list) == 0 {
		// 还没建站点工程：没有任何订单可算 —— 这是正常状态，结果是「零个人」。
		return []int64{}, ""
	}
	from, to := customerSegmentWindow(f, time.Now())
	res, err := h.segments.CustomerSegmentIDsByRange(ctx, &orderdto.CustomerSegmentIDsReq{
		ProjectID: list[0].ID,
		From:      from,
		To:        to,
		Segment:   f.Segment,
		MinOrders: f.MinOrders,
		// 取满上限：这一批 id 要当客户列表的过滤条件用，列表自己还会分页。
		Limit: customerSegmentFilterLimit,
	})
	if err != nil {
		return nil, customerSegmentUnavailableText
	}
	if res == nil {
		return []int64{}, ""
	}
	return res.UserIDs, ""
}

// customerSegmentFilterLimit 列表筛选一次最多取多少个分段 id。
//
// 与订单侧的上限同值：再多也拿不到（那边会截断），与其让页面显示一个「少了人」的
// 筛选结果，不如取满上限、让结果在超过时由用户自己缩小时间范围。
const customerSegmentFilterLimit = 500

// customerRfmSegmentUnavailableText RFM 分段筛不了时的归口文案。
//
// 与消费分段的文案分开：两者可能同时出错，而用户需要知道是哪一维没生效。
const customerRfmSegmentUnavailableText = "按 RFM 分段筛选暂时不可用（数据没接上），请先用其它条件。"

// customerSegmentUnavailableText 分段筛不了时的提示（归口文案，不外泄内部错误）。
const customerSegmentUnavailableText = "按消费分段筛选暂时不可用（数据没接上），请先用其它条件。"

// customerSegmentWindow 分段的默认时间窗口：没给就用本月（与客户概览页默认档一致）。
//
// 与注册时间筛选**不共用**：那是账号的窗口，这是下单的窗口，两者是不同的问题。
// 复用同一个日期控件会让「筛这周注册的新客」这种查询无法表达。
func customerSegmentWindow(f customerFilter, now time.Time) (from, to string) {
	from = strings.TrimSpace(f.SegmentFrom)
	to = strings.TrimSpace(f.SegmentTo)
	if from != "" && to != "" {
		return from, to
	}
	u := now.UTC()
	first := time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(f.SegmentFrom) == "" {
		from = first.Format(utils.LayoutDay)
	}
	if strings.TrimSpace(f.SegmentTo) == "" {
		to = today.Format(utils.LayoutDay)
	}
	return from, to
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
		Segment:        customerPageSegment(c.Query("segment")),
		SegmentFrom:    strings.TrimSpace(c.Query("segmentFrom")),
		SegmentTo:      strings.TrimSpace(c.Query("segmentTo")),
		MinOrders:      customerPageMinOrders(c.Query("minOrders")),
		RfmSegment:     customerPageRfmSegment(c.Query("rfm")),
		TierID:         customerPageTierID(c.Query("tier")),
	}

	// 写动作的结论不在这里回显（走 shell.RenderJump 渲染提示页，见 customerJump）；
	// 页面上的提示条只剩一个来源：本页自己的取数 / 筛选失败。
	pageErr := ""
	// 展示标签与自造文案的取词函数（不再在 Go 里写死中文）。
	tr := shell.TranslateFor(c)

	// 消费分段：先把「谁在这段里」从订单模块要回来，再筛客户行。
	//
	// 失败时**不回落成「不筛」**：回落会让「筛不出来」显示成「全部客户」，
	// 而那正好是运营最可能相信的结果（数字变大了，看起来像筛对了）。
	segIDs, segErr := h.segmentCustomerIDs(ctx, filter)
	if segErr != "" {
		pageErr = customerFirstNonEmpty(pageErr, segErr)
	}
	// RFM 分段是**另一维**：两个筛选都要成立，所以取交集（见 intersectCustomerIDs）。
	rfmIDs, rfmErr := h.rfmSegmentIDs(ctx, filter)
	if rfmErr != "" {
		pageErr = customerFirstNonEmpty(pageErr, rfmErr)
	}
	// 会员等级是**第三维**：三维都要成立，所以逐次求交（每步都保留 nil/空切片 的区别）。
	tierIDs, tierErr := h.tierCustomerIDs(ctx, filter)
	if tierErr != "" {
		pageErr = customerFirstNonEmpty(pageErr, tierErr)
	}
	filterIDs := intersectCustomerIDs(intersectCustomerIDs(segIDs, rfmIDs), tierIDs)

	var list *userdto.CustomerListResp
	switch {
	case h.users == nil:
		// 能力未装配：给出说明而不是 500，也不渲染一个点了必然失败的按钮。
		pageErr = customerFirstNonEmpty(pageErr, userLabelOf(tr, customerUnavailableLabel))
	case segErr != "":
		// 分段没取到就不查列表：查出来的是「全部客户」，而页面上写着「新客」。
		_ = segErr
	case rfmErr != "":
		// RFM 分段同理。**每一个「会取 id 的维度」都要有自己的分支**：漏一个，
		// 那一维失败就会一路走到 default 查出全部客户，而页面上写着那个筛选条件 ——
		// 这正是「筛不出来显示成全部客户」最容易被相信的形态。
		_ = rfmErr
	case tierErr != "":
		_ = tierErr
	default:
		res, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			Keyword:        filter.Keyword,
			Status:         filter.Status,
			EmailVerified:  filter.EmailVerified,
			LockedOnly:     filter.Locked,
			RegisteredFrom: utils.NewJSONTimePtr(customerPageDayStart(filter.RegisteredFrom)),
			RegisteredTo:   utils.NewJSONTimePtr(customerPageDayEnd(filter.RegisteredTo)),
			UserIDs:        filterIDs,
			Offset:         (page - 1) * limit,
			Limit:          limit,
		})
		if err != nil {
			pageErr = customerFirstNonEmpty(pageErr, customerFacingError(c, err))
		} else {
			list = res
		}
	}

	data := customerListPageData(tr, list, filter, page, limit, pageErr, h.users == nil)
	// 等级下拉的选项要现场取（等级是运营自建的，不是常量表）：
	// 取不到时给空切片，页面渲染成只有「不限等级」一项 —— 而不是整个下拉消失。
	data["TierOptions"] = h.membershipTierOptions(ctx)
	data = shell.Prepare(c, data)
	// 写动作的结论不在本页回显（走 shell.RenderJump）；写动作表单 action 上的筛选
	// 上下文由 customerListPageData 组进 ListQuery，不再把整串回跳 URL 塞进隐藏域。
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
	tr := shell.TranslateFor(c)
	id := customerQueryID(c.Query("id"))
	if id == 0 {
		customerListJumpFail(c, userLabelOf(tr, customerInvalidIDLabel))
		return
	}
	// 写动作的结论不在这里回显（走 shell.RenderJump 渲染提示页，见 customerJump）；
	// 页面上的提示条只剩一个来源：本页自己的取数失败。
	pageErr := ""

	if h.users == nil {
		c.HTML(http.StatusOK, "admin/user/customer_detail.html", shell.Prepare(c, customerDetailPageData(
			nil, nil, "", nil, false, false, customerFirstNonEmpty(pageErr, userLabelOf(tr, customerUnavailableLabel)), c)))
		return
	}

	detail, err := h.users.GetCustomer(ctx, id)
	// detail 为 nil 与 err 一样处理：契约的语义是「不存在返回业务错误」，
	// 但页面不能押注在调用方一定这么做 —— 少了这一条，一个 nil 详情就会渲染出一页
	// 全是空值和必然失败按钮的「客户」。
	if err != nil || detail == nil {
		// 客户不存在时不渲染空详情页（它的每个动作都要求一个存在的客户），
		// 而是给一张提示页 + 回列表的链接。
		msg := customerFacingError(c, err)
		if msg == "" {
			msg = shell.TranslateFor(c)(userenums.ErrUserNotFound, "用户不存在")
		}
		customerListJumpFail(c, msg)
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
		customerFirstNonEmpty(pageErr, projectsErr, summaryErr), c)
	applyCustomerMembership(memberData, h.membershipView(ctx, c, selected, id))
	c.HTML(http.StatusOK, "admin/user/customer_detail.html", shell.Prepare(c, memberData))
}

// CustomerStatusSave 启用 / 停用账号（POST /admin/customers/status）。
func (h *customerPageHandle) CustomerStatusSave(c *gin.Context) {
	if h.users == nil {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerUnavailableLabel))
		return
	}
	id := customerQueryID(c.PostForm("customerId"))
	// 目标状态取 toStatus，**不是** status：筛选用的 status 随表单 action 的 query 回跳，
	// 两者同名会让回跳后的列表把「目标状态」当成筛选条件，看起来像「筛选没了」。
	status := customerPageStatus(c.PostForm("toStatus"))
	if id == 0 {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerInvalidIDLabel))
		return
	}
	// 目标状态只接受「正常 / 已停用」两个值（与 service 的白名单一致，但这里先拦一道）：
	// 表单是客户端可伪造的，而待激活一旦能当目标值，就会出现「被手工改成未验证」的账号 ——
	// 它既收不到验证邮件、也没有人能解释它是怎么来的。
	if status != customerStatusActive && status != customerStatusDisabled {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerStatusInvalidLabel))
		return
	}
	res, err := h.users.SetCustomerStatus(c.Request.Context(), &userdto.CustomerStatusReq{
		CustomerID: id,
		Status:     status,
	})
	if err != nil {
		customerJumpFail(c, customerFacingError(c, err))
		return
	}
	// 回执按**目标状态**给：运营点的是「停用」，回执就应该是「账号已停用」。
	customerJumpDone(c, customerNotice(c, customerStatusMessage(res.Status)))
}

// CustomerUnlock 解除登录锁定（POST /admin/customers/unlock）。
func (h *customerPageHandle) CustomerUnlock(c *gin.Context) {
	if h.users == nil {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerUnavailableLabel))
		return
	}
	id := customerQueryID(c.PostForm("customerId"))
	if id == 0 {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerInvalidIDLabel))
		return
	}
	res, err := h.users.UnlockCustomer(c.Request.Context(), &userdto.CustomerUnlockReq{CustomerID: id})
	if err != nil {
		customerJumpFail(c, customerFacingError(c, err))
		return
	}
	// 三种结果各说各的（见 dto 注释）：解除了锁定 / 清了残留计数 / 本来就没事。
	switch {
	case res.Unlocked:
		customerJumpDone(c, customerNotice(c, userenums.MsgCustomerUnlocked))
	case res.Cleared:
		customerJumpDone(c, customerNotice(c, userenums.MsgCustomerFailuresCleared))
	default:
		customerJumpDone(c, customerNotice(c, userenums.MsgCustomerNotLocked))
	}
}

// CustomerBulkStatusSave 批量启用 / 停用（POST /admin/customers/bulk-status）。
//
// 逐条走**同一条单条写入路径**（h.users.SetCustomerStatus，与 /admin/customers/status 一致）：
// 某一条失败不中断整批 —— 整批回滚会让运营以为「一条都没做」，然后反复重试。
// 结果按「已处理 N 个 / 未处理 M 个」渲染进提示页，不做静默的部分成功。
func (h *customerPageHandle) CustomerBulkStatusSave(c *gin.Context) {
	if h.users == nil {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerUnavailableLabel))
		return
	}
	// 目标状态取自 toStatus（筛选用的 status 随表单 action 的 query 回跳，两者同名会让
	// 回跳后的列表看起来「筛选没了」）；只接受「正常 / 已停用」两个值，与单条动作同口径。
	status := customerPageStatus(c.PostForm("toStatus"))
	if status != customerStatusActive && status != customerStatusDisabled {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerStatusInvalidLabel))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		customerJumpFail(c, customerBulkIDsText(c, berr))
		return
	}
	if len(ids) == 0 {
		// 表单是客户端可伪造的：一条都没勾就直接提交是可能的，不能当成功处理。
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerBulkNothingSelectedLabel))
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
	customerJumpDone(c, customerBulkSummary(c, customerStatusActionVerb(c, status), done, skipped))
}

// CustomerBulkUnlock 批量解除登录锁定（POST /admin/customers/bulk-unlock）。
//
// 同一批里三种情况分开计数（真的解开了 / 本来就没事 / 未处理）：
// 把「本来就没事」混进「失败」会让运营以为有账号没解锁成功而去点第二次，
// 混进「成功」则是谎报 —— 而它其实是这条批量指令里最需要被解释的一种结果。
func (h *customerPageHandle) CustomerBulkUnlock(c *gin.Context) {
	if h.users == nil {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerUnavailableLabel))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		customerJumpFail(c, customerBulkIDsText(c, berr))
		return
	}
	if len(ids) == 0 {
		customerJumpFail(c, userLabelOf(shell.TranslateFor(c), customerBulkNothingSelectedLabel))
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
	customerJumpDone(c, customerBulkUnlockSummary(c, unlocked, noop, skipped))
}

// —— 批量结论的文案源（渲染进提示页）——
//
// 形态与 order 模块的 orderBulkText 同构：**key 与中文原文只有这一份**，
// 写侧用它 Sprintf 出整句，经 shell.RenderJump 直接渲染进响应体 —— 不再经 ?done=
// 回带，所以也不需要读侧那份「候选集合 + 数字归一比对」（customerBulkNoticeCandidates
// 已随读侧整批删除）。
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

// customerStatusActionVerbKey 目标状态 → 摘要里动词的词条。
func customerStatusActionVerbKey(status int) customerBulkText {
	if status == customerStatusActive {
		return customerBulkVerbEnabled
	}
	return customerBulkVerbDisabled
}

// —— 写动作的出口：整页提示（对应 ThinkPHP 的 success() / error()）——
//
// 取代原先的 302 + `?err=` / `?ok=` / `?done=`：那条通道要求读侧再判一次「这条提示
// 是不是本仓给的」（customerPageFacingText 白名单、customerBulkNoticeCandidates 的
// 数字归一比对），而查询参数不是可信边界。现在文案走响应体，读侧判定
// （customerPageDone / customerBulkNoticeCandidates / FacingQueryText 调用）随之删除。
//
// 提示文本必须**已过本模块白名单 / 已归口**（customerFacingError / customerNotice /
// customerBulkSummary 的产物），原文只进日志 —— 换个页面呈现不等于可以把 err.Error()
// 铺在页面上。

// customerListBackKeys 列表写动作回跳要带回来的筛选上下文。
//
// 与模板里表单 action 的 query 逐键对应（页面渲染时由 customerListQuery 拼进 action，
// POST 回来由 shell.BackPath 从本次请求的 query 读回）—— 两处必须是同一份，否则会出现
// 「页面把某个筛选拼进去了、回跳时又丢掉」这种只在特定筛选下才暴露的差异。
var customerListBackKeys = []string{
	"keyword", "status", "emailVerified", "locked",
	"registeredFrom", "registeredTo",
	"segment", "minOrders", "rfm", "tier", "segmentFrom", "segmentTo",
	"page", "limit",
}

// customerListQuery 列表上下文 → 查询串（拼进写动作表单的 action）。
//
// 空值丢弃、编码走 url.Values（键有序、产物稳定）—— 与 shell.WithParams 同口径。
// 只带**筛选上下文**，不带任何结论文案：结论走响应体（见 shell.RenderJump）。
func customerListQuery(filter customerFilter, page, limit int) string {
	q := url.Values{}
	for k, v := range customerFilterValues(filter) {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

// customerNoticeFallback 成功回执的中文兜底（词条缺失时显示它，而不是裸 key）。
var customerNoticeFallback = map[string]string{
	userenums.MsgCustomerEnabled:         "账号已启用",
	userenums.MsgCustomerDisabled:        "账号已停用",
	userenums.MsgCustomerUnlocked:        "账号已解除锁定",
	userenums.MsgCustomerFailuresCleared: "账号未处于锁定状态，登录失败计数已清零",
	userenums.MsgCustomerNotLocked:       "该账号没有处于锁定状态，无需解除",
}

// customerNotice 取一条成功回执的当前语言文字（渲染进提示页，见 customerJumpDone）。
func customerNotice(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, customerNoticeFallback[key])
}

// customerWriteBack 写动作的回跳地址与链接文字（列表页 / 详情页两档）。
//
// 表单 action 的 query 里带 `id` 表示这个动作来自**客户详情页**（详情页的写表单
// action 是 `/admin/customers/status?id=…&project=…`）；否则来自列表页。
// 回跳地址由服务端从 query 按白名单读回，页面不再把整串 URL 塞进隐藏域。
func customerWriteBack(c *gin.Context) (back, backText string) {
	if strings.TrimSpace(c.Query("id")) != "" {
		return shell.BackPath(c, customerDetailPath, "id", "project"),
			userLabelOf(shell.TranslateFor(c), customerDetailPageTitleLabel)
	}
	return shell.BackPath(c, customerListPath, customerListBackKeys...),
		userLabelOf(shell.TranslateFor(c), customerPageTitleLabel)
}

// customerListBack 回客户列表的地址与链接文字（详情页 GET 出错时用：此时请求 URL 上
// 带着 `id`，不能走 customerWriteBack 那条「有 id 就回详情页」的分档）。
func customerListBack(c *gin.Context) (back, backText string) {
	return shell.BackPath(c, customerListPath, customerListBackKeys...),
		userLabelOf(shell.TranslateFor(c), customerPageTitleLabel)
}

// customerJump 渲染整页提示（htmx 档由 shell.RenderJump 换成 HX-Redirect）。
//
// 成功 1 秒后自动回跳；失败不自动跳（Seconds=0）：运营要看清楚原因。
func customerJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// customerJumpDone 写成功：整页提示，1 秒后自动回跳。
func customerJumpDone(c *gin.Context, msg string) {
	back, backText := customerWriteBack(c)
	customerJump(c, true, msg, back, backText)
}

// customerJumpFail 写失败：整页提示，不自动跳转。
func customerJumpFail(c *gin.Context, msg string) {
	back, backText := customerWriteBack(c)
	customerJump(c, false, msg, back, backText)
}

// customerListJumpFail 详情页 GET 出错的提示页：回跳目标恒为列表页。
func customerListJumpFail(c *gin.Context, msg string) {
	back, backText := customerListBack(c)
	customerJump(c, false, msg, back, backText)
}

// rfmSegmentIDs 把「RFM 分段 + 时间窗口」换成一批客户 id（三态同 segmentCustomerIDs）。
//
// 与消费分段走**两套**不同的分段：那三个看下单行为（新客 / 回头客 / 复购），
// 这一条看 RFM 总分。两者同时给出时调用方取交集（见 intersectCustomerIDs）。
func (h *customerPageHandle) rfmSegmentIDs(ctx context.Context, f customerFilter) ([]int64, string) {
	if f.RfmSegment == "" {
		return nil, ""
	}
	// 端口没接、没有工程、取数失败：都明确说「筛不了」，而不是当作没筛
	//（当作没筛会让「筛不出来」显示成「全部客户」，而那正是用户最可能相信的结果）。
	if h.rfm == nil || h.projects == nil {
		return nil, customerRfmSegmentUnavailableText
	}
	list, perr := h.projects.List(ctx)
	if perr != nil {
		return nil, customerRfmSegmentUnavailableText
	}
	if len(list) == 0 {
		// 还没建站点工程：没有任何订单可算 —— 正常状态，结果是「零个人」。
		return []int64{}, ""
	}
	from, to := customerSegmentWindow(f, time.Now())
	res, err := h.rfm.CustomerRfmSegmentIDsByRange(ctx, &orderdto.CustomerRfmSegmentIDsReq{
		ProjectID: list[0].ID,
		From:      from,
		To:        to,
		Segment:   f.RfmSegment,
		// 取满上限：这一批 id 要当客户列表的过滤条件用，列表自己还会分页。
		Limit: customerSegmentFilterLimit,
	})
	if err != nil {
		return nil, customerRfmSegmentUnavailableText
	}
	ids := res.UserIDs
	if ids == nil {
		ids = []int64{}
	}
	return ids, ""
}

// intersectCustomerIDs 两个 id 列表的交集；nil 表示「这一维不筛」。
//
// 三态比两态更容易写错，所以规则写死在这里：
//   - 两个都是 nil（都不筛）      → nil（不过滤）；
//   - 只有一个是 nil              → 另一个（含空切片，空切片必须保持非 nil）；
//   - 两个都不是 nil              → 交集（任一为空则交集为空）。
//
// 中间那条是关键的：**nil 与空切片不能互相顶替** —— nil 一路传到 model 的
// `id IN (...)` 之前会被短路成「不过滤」，空切片被短路成「没人」。
func intersectCustomerIDs(a, b []int64) []int64 {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	}
	inA := make(map[int64]bool, len(a))
	for _, id := range a {
		inA[id] = true
	}
	out := make([]int64, 0, len(b))
	for _, id := range b {
		if inA[id] {
			out = append(out, id)
		}
	}
	return out
}

// 会员等级筛选的边界。
const (
	// membershipFilterPageSize 一次向会员模块要多少人（200 是会员模块自己的单页上限）。
	membershipFilterPageSize = 200
	// membershipFilterMaxPages 最多翻几页。
	//
	// 客户列表把这一批 id 当过滤条件用，所以要尽量取全；但「取全」在极端情况下
	// （某等级几万人）会变成一次几十页的循环。翻到上限时**必须明说**，不能静静地
	// 只筛前 N 个人 —— 那种结果看起来完全正常，只是少了一批人。
	membershipFilterMaxPages = 5
)

// membershipFilterTruncatedText 达到翻页上限时的归口文案。
//
// 达到上限时**整维当作失败**（不交出那部分 id）：交出一半 id 会让页面显示一个
// 看起来正常、只是少了一批人的列表 —— 而用户无从知道少了。明说「筛不了、请收窄」
// 是唯一不会静默出错的形态。
const membershipFilterTruncatedText = "该会员等级的人数超过筛选上限，无法作为筛选项使用。请配合其它条件一起筛。"

// tierCustomerIDs 把「会员等级」换成一批客户 id（三态同 segmentCustomerIDs）。
func (h *customerPageHandle) tierCustomerIDs(ctx context.Context, f customerFilter) ([]int64, string) {
	if f.TierID <= 0 {
		return nil, ""
	}
	if h.membershipAdmin == nil || h.projects == nil {
		return nil, membershipFilterUnavailableText
	}
	list, perr := h.projects.List(ctx)
	if perr != nil {
		return nil, membershipFilterUnavailableText
	}
	if len(list) == 0 {
		// 还没建站点工程：没有任何归属可算 —— 正常状态，结果是「零个人」。
		return []int64{}, ""
	}
	ids := make([]int64, 0, membershipFilterPageSize)
	for page := 1; page <= membershipFilterMaxPages; page++ {
		rows, err := h.membershipAdmin.ListAssignments(ctx, &membershipdto.ListAssignmentsReq{
			ProjectID: list[0].ID,
			TierID:    f.TierID,
			Page:      page,
			Size:      membershipFilterPageSize,
		})
		if err != nil {
			return nil, membershipFilterUnavailableText
		}
		for _, row := range rows {
			ids = append(ids, int64(row.UserID))
		}
		if len(rows) < membershipFilterPageSize {
			return ids, ""
		}
	}
	// 翻满上限还没取完：整维作失败（见 membershipFilterTruncatedText 的说明）。
	return nil, membershipFilterTruncatedText
}

// membershipFilterUnavailableText 会员等级筛选不可用时的归口文案。
const membershipFilterUnavailableText = "按会员等级筛选暂时不可用（会员模块没接上），请先用其它条件。"

// membershipTierOptions 客户列表的等级下拉（按 id 升序，与会员模块的展示顺序一致）。
//
// 取不到时返回空切片（页面渲染成只有一个「不限等级」的选项），**不返回 nil** ——
// 模板会 range 它，而 Jet 对 nil 求 range 是运行时错误（整页 500）。
func (h *customerPageHandle) membershipTierOptions(ctx context.Context) []gin.H {
	out := []gin.H{}
	if h.membershipTiers == nil || h.projects == nil {
		return out
	}
	list, perr := h.projects.List(ctx)
	if perr != nil || len(list) == 0 {
		return out
	}
	tiers, err := h.membershipTiers.ListTiers(ctx, &membershipdto.ListTiersReq{ProjectID: list[0].ID})
	if err != nil {
		return out
	}
	for _, t := range tiers {
		// ID 也做成字符串：模板里要与 FilterTier（字符串）比较，
		// 而 Jet 对 int64 与 string 的 `==` 不会隐式转换（比出来恒为 false，
		// 表现是「选中态永远停在第一项」，页面不报错）。
		out = append(out, gin.H{"ID": strconv.FormatInt(t.ID, 10), "Name": t.Name})
	}
	return out
}

// customerOverviewPath 页面路径（菜单 seed 与导航高亮都用它）。
const customerOverviewPath = "/admin/customers/overview"

// customerOverviewDefaultPreset 默认档位。
//
// 取「本月」而不是「今天」：客户的增长是按周按月才看得出形状的，默认给一天会让
// 新客数常年是 0 或 1，页面看起来像坏了。
const customerOverviewDefaultPreset = "month"

// customerOverviewPresets 可选档位（顺序即展示顺序）。
//
// 复用概览页那批时间词条（`admin.dashboard.range.*`）：同一批档位在后台出现两次，
// 各写一套文案的话迟早出现「两页的『本周』不是同一个意思」—— 而那是个没人会去核对的地方。
//
// 与前端下拉（ui/datepreset.js 的 rangeOf 分支）**必须是一套档位名**：下拉选中后由脚本把
// 两个 date 填好再提交，所以正常路径根本不经过这里；但老书签 / 手改 URL 里的 ?range=xxx
// 仍走本表，两处名字对不上就会静默落进「认不出 → 回落默认档」。
var customerOverviewPresets = []string{"today", "yesterday", "week", "month", "lastMonth", "days7", "days30", "year"}

// customerOverviewRangeMaxDays 显式区间的跨度上限（含首尾）。
//
// 与订单概览同口径：客户增长要扫 orders 的区间聚合，一条 URL 不该让库替人做无限期扫描。
// 超出就收敛（不是报错）—— 用户多半是手改 URL 或书签过期，给他一个「能看的结果 + 实际窗口」
// 比一个错误页有用；页面上本来就渲染着 Range.From ~ Range.To，收敛是可见的。
const customerOverviewRangeMaxDays = 366

// customerOverviewTitleLabel 页标题（词条 key + 兜底文案）。
//
// 用 userLabel 而不是直接写字面 key：模板壳读的是 data["title"]，而 injectI18n 只翻
// **非空字符串**。给裸 key 时若该词条恰好没 seed 进库，顶栏会显示 "admin.customer.overview.title"。
var customerOverviewTitleLabel = userLabel{"admin.customer.overview.title", "客户概览"}

// customerOverviewRange 生效窗口（含首尾，UTC 日界，与订单聚合同口径）。
type customerOverviewRange struct {
	Key  string
	From string
	To   string
}

// customerOverviewRangeOf 把一次请求换算成生效窗口。
//
// **显式区间（?from=&to=）优先于档位名**：后台统一的时间筛选条（admin/partials/date_filter.html）
// 无论选哪个档位都会把两个 date 填好再提交，所以正常路径带的是 from/to；?range=xxx 只服务于
// 老书签与手改 URL，是兜底而不是主路径。
//
// 两条路径的日界口径**不同，这是有意的**：
//
//	· 显式区间来自用户亲自选的两个日期，窗口就是他选的那两天（组件的「昨日」= 昨天 ~ 昨天）；
//	· 档位名由服务端算，`end` 恒为「今天」（本周期至今，与订单页的档位口径一致）。
//
// 不去统一它们：档位名的语义是「本周期至今」，而显式区间是「这两天」—— 强行统一会让
// 其中一边变成另一个意思。
func customerOverviewRangeOf(key, fromQ, toQ string, now time.Time) customerOverviewRange {
	if r, ok := customerOverviewExplicitRange(fromQ, toQ, now); ok {
		return r
	}

	today := customerOverviewDayStart(now)
	switch key {
	case "today":
		key = "today"
	case "yesterday":
		key = "yesterday"
		today = today.AddDate(0, 0, -1)
	case "week":
		today = today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	case "year":
		today = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "lastMonth":
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	case "days7":
		today = today.AddDate(0, 0, -6)
	case "days30":
		today = today.AddDate(0, 0, -29)
	case "month":
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		key = customerOverviewDefaultPreset
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	// 结束日永远是「今天」（UTC）：week / month / year 三个档位是「本周期至今」。
	end := customerOverviewDayStart(now)
	return customerOverviewRange{
		Key:  key,
		From: today.Format(utils.LayoutDay),
		To:   end.Format(utils.LayoutDay),
	}
}

// customerOverviewExplicitRange 解析 ?from= / ?to=，两端都为空时返回 ok=false 走档位名。
//
// 收敛规则（都是「给一个能看的结果」而不是报错，理由见 customerOverviewRangeMaxDays）：
//
//	· 只给一端 → 另一端补成「今天」（用户想表达的是「从这天起」或「到这天为止」）；
//	· 解析不出来 → 忽略这一端（当作没传，而不是把整页变成错误页）；
//	· 起点晚于终点 → 两端对调（用户选反了很常见，对调后正是他想要的那个区间）；
//	· 终点超今天 → 收敛到今天（未来的客户增长没有意义，留着只会得出一片 0）；
//	· 跨度超上限 → 起点收到「终点 - 上限 + 1 天」。
//
// 判据是同口径的 `customerOverviewDayStart`：所有比较都落在 UTC 日界上，
// 本地时区会让「今天的订单」与「今天这个客户」在跨零点前后错开一个时区的量。
func customerOverviewExplicitRange(fromQ, toQ string, now time.Time) (customerOverviewRange, bool) {
	if fromQ == "" && toQ == "" {
		return customerOverviewRange{}, false
	}
	today := customerOverviewDayStart(now)

	from, fromOK := customerOverviewParseDay(fromQ)
	to, toOK := customerOverviewParseDay(toQ)
	if !fromOK && !toOK {
		// 两端都写坏（或只剩空串）→ 当作没传，交给档位名，不要让整页变错误页。
		return customerOverviewRange{}, false
	}
	if !fromOK {
		from = today
	}
	if !toOK {
		to = today
	}
	if from.After(to) {
		from, to = to, from
	}
	if to.After(today) {
		to = today
	}
	if from.After(to) {
		// 收敛终点之后起点可能反超（起点在未来）：把起点也拉回终点。
		from = to
	}
	if maxFrom := to.AddDate(0, 0, -(customerOverviewRangeMaxDays - 1)); from.Before(maxFrom) {
		from = maxFrom
	}
	return customerOverviewRange{
		Key:  "custom",
		From: from.Format(utils.LayoutDay),
		To:   to.Format(utils.LayoutDay),
	}, true
}

// customerOverviewParseDay 解析一个 ISO 日期（只有日期，没有时刻）。
func customerOverviewParseDay(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	d, err := time.Parse(utils.LayoutDay, s)
	if err != nil {
		return time.Time{}, false
	}
	return d.UTC(), true
}

// customerOverviewDayStart 取某个时刻所在 UTC 日的零点。
//
// 与 order service 的 dayStart 同名同义，但**刻意各写一份**：两处对「日界」的定义
// 将来若分叉（订单能查任意历史、客户域可能引入本地时区偏好），共用一份会把它们绑死。
// 真要收口，落点应是 pkg/utils，而不是让 user 模块 import order 的 service。
func customerOverviewDayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// customerRangeQuery 把生效区间拼成 query 片段（from=…&to=…），供同页其它链接回带区间。
//
// 刻意**不用 ?range=<档位名>**：页面上的时间筛选条（admin/partials/date_filter.html）
// 无论选哪个档位都会把两个 date 填好再提交，所以服务端的 rng.Key 在正常路径上恒为 "custom" ——
// 用 range=custom 回带等于什么都没带，翻页 / 切分段 / 切视图时会静默变回默认区间，
// 而每一处单独看都「有回带区间」的代码、也都不报错。
func customerRangeQuery(rng customerOverviewRange) string {
	return "from=" + rng.From + "&to=" + rng.To
}

// customerOverviewPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
//
// 与客户列表页同一条理由：渲染键名只在这里定义一次，真实渲染测试可以直接喂数据
// 走同一条组装路径，不必在测试里手抄一份键名。
//
// 不再有 `Presets` 键：三页（概览 / 群组 / RFM）的时间筛选条已统一成
// admin/partials/date_filter.html 组件，档位下拉由它自己渲染 —— 服务端拼胶囊链接的那套
// 逻辑（customerOverviewPresetLinks）随之删除，留着就是第二份会漂移的真源。
func customerOverviewPageData(tr func(key, fallback string) string, rng customerOverviewRange, growth *orderdto.CustomerGrowthResp, errText string) gin.H {
	data := gin.H{
		"title": userLabelOf(tr, customerOverviewTitleLabel),
		"Path":  customerOverviewPath,
		"Range": rng,
		"Err":   errText,
		// GrowthReady 与「有数据」是两件事：未接线时给一句人话，而不是一片 0
		//（0 会被读成「这段时间一个客户都没来」）。同其它页面的降级口径。
		"GrowthReady": growth != nil,
	}
	if growth == nil {
		return data
	}
	data["OrderingCustomers"] = growth.OrderingCustomers
	data["NewCustomers"] = growth.NewCustomers
	data["ReturningCustomers"] = growth.ReturningCustomers
	data["Repurchasers"] = growth.Repurchasers
	data["NewRepurchasers"] = growth.NewRepurchasers
	data["RepurchaseRateLabel"] = growth.RepurchaseRateLabel
	return data
}

// CustomerOverviewPage 客户概览（GET /admin/customers/overview）。
func (h *customerPageHandle) CustomerOverviewPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())

	// 工程：客户增长是工程维度的（同一批人在两个站点上是两件事），
	// 取值规则与详情页逐字一致 —— 先看 ?project=，没有就用第一个。
	// 本页**不渲染工程切换器**：这一页回答的是「这段时间客户怎么变」，
	// 工程由从列表页带过来的上下文决定；多一个下拉只是多一个能选错的控件。
	var projects []projectcontract.ProjectResp
	if h.projects != nil {
		if list, perr := h.projects.List(ctx); perr == nil {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var growth *orderdto.CustomerGrowthResp
	var errText string
	switch {
	case h.growth == nil:
		errText = userLabelOf(tr, customerUnavailableLabel)
	case selected == "":
		// 没有工程就没有订单可算 —— 「还没建站点工程」是正常状态，不是错误。
		// 留空：模板会把 GrowthReady=false 渲染成一句空态说明，而不是一片 0。
		errText = ""
	default:
		res, err := h.growth.CustomerGrowthByRange(ctx, &orderdto.CustomerGrowthReq{
			ProjectID: selected,
			From:      rng.From,
			To:        rng.To,
		})
		if err != nil {
			errText = customerFacingError(c, err)
		} else {
			growth = res
		}
	}

	data := shell.Prepare(c, customerOverviewPageData(tr, rng, growth, errText))
	c.HTML(200, "admin/user/customer_overview", data)
}

// customerRfmPath 页面路径（菜单 seed 与导航高亮都用它）。
const customerRfmPath = "/admin/customers/rfm"

// customerRfmTitleLabel 页标题（词条 key + 兜底文案）。
var customerRfmTitleLabel = userLabel{"admin.customer.rfm.title", "RFM 分析"}

// customerRfmSegmentLabel 分段 → 词条 key 与兜底文案。
//
// 与订单侧的白名单同字面量（那边是唯一的判定处，这里只做展示映射）：
// 认不出的分段原样显示，而不是渲染成空白 —— 口径变了而这里没跟上时，
// 页面上出现一个陌生的英文词比出现一个空单元格更容易被发现。
func customerRfmSegmentText(tr func(key, fallback string) string, segment string) string {
	switch segment {
	case "vip":
		return tr("admin.customer.rfm.segment.vip", "高价值")
	case "potential":
		return tr("admin.customer.rfm.segment.potential", "潜力")
	case "low_value":
		return tr("admin.customer.rfm.segment.lowValue", "一般")
	}
	return segment
}

// customerRfmRow 明细表的一行（客户资料 + RFM 分数）。
type customerRfmRow struct {
	UserID        int64
	Name          string
	Email         string
	DetailURL     string
	LastOrderAt   string
	RecencyDays   int
	Frequency     int64
	MonetaryLabel string
	RScore        int
	FScore        int
	MScore        int
	TotalScore    int
	Segment       string
	SegmentLabel  string
}

// customerRfmPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
func customerRfmPageData(tr func(key, fallback string) string, rng customerOverviewRange,
	res *orderdto.CustomerRfmResp, rows []customerRfmRow,
	segment string, page, limit int, total int64, errText string) gin.H {
	data := gin.H{
		"title":   userLabelOf(tr, customerRfmTitleLabel),
		"Path":    customerRfmPath,
		"Range":   rng,
		"Segment": segment,
		"Err":     errText,
		"Rows":    rows,
		"Page":    page,
		"Limit":   limit,
		"Total":   total,
		// RfmReady 与「有数据」是两件事：未接线时给一句人话，而不是三格 0
		//（0 会被读成「这段时间一个客户都没有」）。同其它页面的降级口径。
		"RfmReady": res != nil,
	}
	if res == nil {
		return data
	}
	data["Customers"] = res.Customers
	data["Vip"] = res.Vip
	data["Potential"] = res.Potential
	data["LowValue"] = res.LowValue
	return data
}

// customerRfmSegmentTabs 分段筛选徽章（与客户列表的计数徽章同一条口径：
// 一个徽章一个链接，URL 由服务端拼好，条件之间可叠加）。
func customerRfmSegmentTabs(tr func(key, fallback string) string, res *orderdto.CustomerRfmResp, active string, rng customerOverviewRange) []gin.H {
	if res == nil {
		return nil
	}
	mk := func(key, labelKey, label string, count int64) gin.H {
		url := customerRfmPath + "?" + customerRangeQuery(rng)
		if key != "" {
			url += "&segment=" + key
		}
		return gin.H{
			"Key": key, "LabelKey": labelKey, "Label": label,
			"Count": count, "Active": key == active,
			"Badge": customerRfmBadge(key), "URL": url,
		}
	}
	return []gin.H{
		mk("", "admin.customer.rfm.tab.all", "全部", res.Customers),
		mk("vip", "admin.customer.rfm.segment.vip", "高价值", res.Vip),
		mk("potential", "admin.customer.rfm.segment.potential", "潜力", res.Potential),
		mk("low_value", "admin.customer.rfm.segment.lowValue", "一般", res.LowValue),
	}
}

// customerRfmBadge 分段 → 徽章色调（与列表页的计数徽章同一套类名）。
func customerRfmBadge(key string) string {
	switch key {
	case "vip":
		return "badge-success"
	case "potential":
		return "badge-info"
	case "low_value":
		return "badge-mute"
	}
	return ""
}

// customerRfmPageSegment 分段查询值 → 已知分段（认不出一律回落「全部」）。
func customerRfmPageSegment(v string) string {
	switch v {
	case "vip", "potential", "low_value":
		return v
	}
	return ""
}

// CustomerRfmPage RFM 分析（GET /admin/customers/rfm）。
func (h *customerPageHandle) CustomerRfmPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	rng := customerOverviewRangeOf(c.Query("range"), c.Query("from"), c.Query("to"), time.Now())
	segment := customerRfmPageSegment(c.Query("segment"))
	page, limit := shell.PageParams(c)
	if limit > 0 && limit > rfmMaxPageSize {
		limit = rfmMaxPageSize
	}

	var res *orderdto.CustomerRfmResp
	var errText string
	switch {
	case h.rfm == nil:
		errText = customerRfmUnavailableText
	case h.projects == nil:
		errText = customerRfmUnavailableText
	default:
		list, perr := h.projects.List(ctx)
		if perr != nil {
			errText = customerRfmUnavailableText
			break
		}
		if len(list) == 0 {
			// 还没建站点工程：没有订单可算 —— 正常状态，结果是空报表。
			errText = ""
			break
		}
		out, rerr := h.rfm.CustomerRfmByRange(ctx, &orderdto.CustomerRfmReq{
			ProjectID: list[0].ID,
			From:      rng.From,
			To:        rng.To,
			Segment:   segment,
			Limit:     limit,
			Offset:    (page - 1) * limit,
		})
		if rerr != nil {
			errText = customerRfmUnavailableText
		} else {
			res = out
		}
	}

	rows := h.customerRfmRows(ctx, tr, res)
	var total int64
	if res != nil {
		total = res.Total
	}
	data := shell.Prepare(c, customerRfmPageData(tr, rng, res,
		rows, segment, page, limit, total, errText))
	data["SegmentTabs"] = customerRfmSegmentTabs(tr, res, segment, rng)
	// 分页链接保留区间与分段：不带的话翻页会静默变成「全部区间 + 全部分段」。
	base := customerRfmPath + "?" + customerRangeQuery(rng)
	if segment != "" {
		base += "&segment=" + segment
	}
	for k, v := range shell.BuildPagination(total, page, limit, base, tr).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/user/customer_rfm", data)
}

// customerRfmRows 把 RFM 明细补上客户资料（姓名 / 邮箱 / 详情链接）。
//
// 取不到资料时**保留这一行并显示 id**：整行丢掉会让表格少人而没有任何提示，
// 而「订单侧有这个人、客户侧查不到」正是需要被看见的异常（账号被注销）。
func (h *customerPageHandle) customerRfmRows(ctx context.Context, tr func(key, fallback string) string, res *orderdto.CustomerRfmResp) []customerRfmRow {
	if res == nil || len(res.Items) == 0 {
		return nil
	}
	names := map[int64]customerRfmRow{}
	if h.users != nil {
		ids := make([]int64, 0, len(res.Items))
		for _, it := range res.Items {
			ids = append(ids, it.UserID)
		}
		if list, err := h.users.ListCustomers(ctx, &userdto.CustomerListReq{
			UserIDs: ids, Limit: len(ids),
		}); err == nil && list != nil {
			for _, c := range list.List {
				names[int64(c.ID)] = customerRfmRow{Name: c.DisplayName, Email: c.Email}
			}
		}
	}
	rows := make([]customerRfmRow, 0, len(res.Items))
	for _, it := range res.Items {
		row := customerRfmRow{
			UserID:        it.UserID,
			LastOrderAt:   it.LastOrderAt,
			RecencyDays:   it.RecencyDays,
			Frequency:     it.Frequency,
			MonetaryLabel: it.MonetaryLabel,
			RScore:        it.RScore,
			FScore:        it.FScore,
			MScore:        it.MScore,
			TotalScore:    it.TotalScore,
			Segment:       it.Segment,
			SegmentLabel:  customerRfmSegmentText(tr, it.Segment),
			DetailURL:     customerListPath + "/detail?id=" + strconv.FormatInt(it.UserID, 10),
		}
		if info, ok := names[it.UserID]; ok {
			row.Name = info.Name
			row.Email = info.Email
		}
		rows = append(rows, row)
	}
	return rows
}

// rfmMaxPageSize 明细分页上限（与订单侧的上限同值）。
const rfmMaxPageSize = 200

// customerRfmUnavailableText 取数不可用时的提示（归口文案，不外泄内部错误）。
const customerRfmUnavailableText = "RFM 分析暂时不可用（数据没接上），请稍后再试。"

// customer_view.go - 客户管理页的视图构造（列表/详情数据、行视图、状态选项与标签）。

// customerListPageData 组装列表页渲染数据（纯函数：不取数、不依赖 gin.Context 之外的东西）。
//
// 抽出来的理由同订单页：渲染键名与计数口径只在这里定义一次，
// 真实渲染测试可以直接喂数据走同一条组装路径，不必在测试里手抄一份键名
// （手抄的那份会随模板演进静默失配，而那正是「页面上少了一块、断言却通过」的成因）。
func customerListPageData(tr func(key, fallback string) string, list *userdto.CustomerListResp, filter customerFilter,
	page, limit int, pageErr string, capabilityMissing bool) gin.H {
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
		"FilterSegment":     filter.Segment,
		"FilterSegmentFrom": filter.SegmentFrom,
		"FilterSegmentTo":   filter.SegmentTo,
		"FilterMinOrders":   customerMinOrdersQueryValue(filter.MinOrders),
		"FilterRfm":         filter.RfmSegment,
		"FilterTier":        customerTierQueryValue(filter.TierID),
		// TierOptions 在这里给空切片兜底（handler 会覆盖成真实选项）。
		// 渲染测试直接调本函数，而模板 `{{range _, t := tiers}}` 对缺席键（nil）
		// 是**运行时错误** → 整页 500，错误信息只有模板行号。
		// 同一个坑在客户概览/群组留存的 Columns 上踩过一次：模板会 range / len 的键，
		// 组装函数必须给零值，不能指望调用方记得设。
		"TierOptions":       []gin.H{},
		"CapabilityMissing": capabilityMissing,
		"Err":               pageErr,
		// ListQuery 写动作表单 action 上的筛选上下文（POST 回来由 shell.BackPath
		// 从 query 读回）。放在组装函数里：渲染测试直接调本函数也能拿到，
		// 不必在测试里手抄一份「拼进 action 的查询串」。
		"ListQuery": customerListQuery(filter, page, limit),
	}
}

// customerDetailPageData 组装详情页渲染数据。
//
// 四个「给运营看的说明」都是显式布尔 + 文案，而不是让模板去判断 nil：
// Jet 里判断一个可能为 nil 的接口值很容易写成「看起来对、渲染出来是空块」。
func customerDetailPageData(detail *userdto.CustomerResp, projects []projectcontract.ProjectResp,
	selected string, summary *ordercontract.CustomerOrderSummaryResp,
	projectsFailed, summaryFailed bool, pageErr string, c *gin.Context) gin.H {
	data := gin.H{
		"title":           userLabelOf(shell.TranslateFor(c), customerDetailPageTitleLabel),
		"menu":            "customers",
		"Err":             pageErr,
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
		// 消费分段三个参数：翻页与计数器链接都必须带上，否则「翻到第二页」
		// 或「点一下状态计数」会静默丢掉分段筛选，列表变回全部客户。
		"segment":     filter.Segment,
		"minOrders":   customerMinOrdersQueryValue(filter.MinOrders),
		"rfm":         filter.RfmSegment,
		"tier":        customerTierQueryValue(filter.TierID),
		"segmentFrom": filter.SegmentFrom,
		"segmentTo":   filter.SegmentTo,
	}
}

// customerTierQueryValue 会员等级 → 查询参数值（不筛就不写进 URL）。
func customerTierQueryValue(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// customerMinOrdersQueryValue 复购次数档位 → 查询参数值（不筛就不写进 URL）。
func customerMinOrdersQueryValue(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
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

// Handle 用户侧 HTTP handler。
type Handle struct {
	svc *userservice.Service
}

// NewHandle 构造。
func NewHandle(svc *userservice.Service) *Handle { return &Handle{svc: svc} }

// pageTitles 模板名 → 浏览器标题（key + 中文兜底）。
//
// 集中一张表而不是让每个 handler 各自传：这张表同时是「访客侧有哪些页面」的清单，
// 新增页面时若忘了在这里登记，页面标题会退化成站点名 —— 一眼可见，不会静默出错。
//
// 值不是裸中文：<title> 是**直接渲染**的文本，硬写中文等于英文界面里标题永远中文。
var pageTitles = map[string]userLabel{
	"user/login":         {"user.page.login", "登录"},
	"user/register":      {"user.page.register", "注册"},
	"user/register_done": {"user.page.register_done", "注册成功"},
	"user/forgot":        {"user.page.forgot", "找回密码"},
	"user/reset":         {"user.page.reset", "重置密码"},
	"user/message":       {"user.page.message", "提示"},
	"user/account":       {"user.page.account", "账号中心"},
}

// userTextOf 访客页文案的取词入口（等价于 userLabelOf(shell.TranslateFor(c), l)）。
func userTextOf(c *gin.Context, l userLabel) string {
	return userLabelOf(shell.TranslateFor(c), l)
}

// userTextFilled 取词并按命名参数填充 `{name}` 占位符（复用 pkg/i18n 的实现）。
//
// 词条被改坏（填完仍有残留占位符）时 FillTranslate 自动回落中文兜底再填一次；
// 参数缺失也不补默认值：补 0 会渲染出一句「看起来像结论」的错话，
// 而页面显示代码里的原文至少能让人看出「这句没配好」。
func userTextFilled(c *gin.Context, l userLabel, kv map[string]string) string {
	return i18n.FillTranslate(shell.TranslateFor(c), l.key, l.fallback, kv)
}

// render 渲染访客页面，自动补齐 CSRF token 与当前登录用户。
//
// 每个页面都要拿到这两样，逐个 handler 手写必然会漏掉一个 —— 漏掉 CSRF token 的表现是
// 「页面能打开，一提交就 403」，而报错信息指向中间件，排查会绕很远。
func (h *Handle) render(c *gin.Context, status int, name string, data gin.H) {
	if data == nil {
		data = gin.H{}
	}
	if _, ok := data["csrf_token"]; !ok {
		token, err := builtin.EnsureCSRFTokenWith(c, userCSRFStore{})
		if err != nil {
			logger.Scene("user").Error(err, "生成访客 CSRF token 失败")
		}
		data["csrf_token"] = token
	}
	if _, ok := data["user"]; !ok {
		data["user"] = currentSession(c)
	}
	// 界面语言与取词函数同源（response.RequestLanguage 的判定链：语言 Cookie → query lang
	// → Accept-Language → 默认语言）。两者分家会出现「正文按请求语言取词、<html lang>
	// 恒 zh-CN」这类只在部分页面显现的不一致。
	if _, ok := data["lang"]; !ok {
		data["lang"] = response.RequestLanguage(c)
	}
	// i18n 取词函数（与后台 shell.Prepare 注入的 data["t"] 同源）：访客页面模板用
	// {{ .["t"]("user.x", "中文兜底") }} 取词。
	//
	// **必须在这里无条件注入**：chain 索引 + 函数调用在缺 t 时是静默空串
	// （不报错、不 500、无日志），漏掉的后果是整页文案一起变空白
	// （见 internal/templates/CLAUDE.md「缺键的两种后果」）。
	if _, ok := data["t"]; !ok {
		data["t"] = shell.TranslateFor(c)
	}
	// 书写方向（审计 I18N-02）：访客页面与静态产物用同一条规则
	// （builder.DirAttr：RTL 才落字节，LTR 是 HTML 缺省）。判据各写一份就会出现
	// 「静态页是 rtl、账号页不是」这类只在部分页面显现的方向错误。
	if _, ok := data["dir"]; !ok {
		lang, _ := data["lang"].(string)
		data["dir"] = builder.DirAttr(lang)
	}
	if _, ok := data["site"]; !ok {
		data["site"] = "go_wp"
	}
	if _, ok := data["title"]; !ok {
		data["title"] = userLabelOf(shell.TranslateFor(c), pageTitles[name])
	}
	c.HTML(status, name, data)
}

// userFacingText 白名单判定：命中返回原文（**item_key**），未命中返回空串。
//
// 只做判定、不取词。白名单而非黑名单：service 的业务错误全部来自 userenums，
// 而数据库 / Redis 的错误原文可能带表名与 SQL 片段。判定只有这一份，
// 页面出口（userPageMessage / userKeyText）与接口面共用同一张 UserFacingMessages。
func userFacingText(raw string) string {
	msg := raw
	for _, m := range userenums.UserFacingMessages {
		if prefix, _, found := strings.Cut(m, "%s"); found {
			// 带参数的文案（如锁定剩余时间）只比较 %s 之前的部分。
			if strings.HasPrefix(msg, prefix) {
				return msg
			}
			continue
		}
		if msg == m {
			return msg
		}
	}
	return ""
}

// userPageMessage 页面出口的错误文案归口（白名单判定 + **取当前语言的译文**）。
//
// 为什么页面路径必须多这一层取词：userenums 的值是 i18n **item_key**
// （user.err.usernameTaken / user.msg.customerDisabled …），而访客页面
// （user/register、user/account、user/message）里的 {{.error}} / {{.message}} 是
// **直接渲染**的文本，不经过 pkg/response 的 translate —— 只放行 key 的话，
// 访客注册失败看到的是「user.err.usernameTaken」，而不是「用户名已被占用」。
//
// 命中 → 取词；未命中 → 记结构化日志 + 归口文案（原文只进日志）。
// 取词函数据 fallback 原样返回 key：词条缺失时页面显示 key（一眼可见），不静默吞掉整句。
func userPageMessage(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if hit := userFacingText(err.Error()); hit != "" {
		return shell.TranslateFor(c)(hit, hit)
	}
	logger.Scene("user").
		With("path", c.Request.URL.Path).
		Error(err, "用户模块出现未归类错误")
	return shell.TranslateFor(c)(userenums.ErrInternal, "操作失败，请稍后重试")
}

// userKeyText 参数级提示的取词出口：没有 error 对象、文案就是 enums 里的某个常量
// （缺 device 标识、登出失败归口等场景直接传常量给模板）。
//
// 与 userPageMessage 同源（同一份 userFacingText 判定）：白名单内的 key 直接渲染时
// 同样不能裸出。ErrInternal 不在白名单里，会落归口文案 —— 这正是它该有的语义
// （归口文案是「未命中时的返回值」，不是业务文案）。
func userKeyText(c *gin.Context, key string) string {
	if hit := userFacingText(key); hit != "" {
		return shell.TranslateFor(c)(hit, hit)
	}
	return shell.TranslateFor(c)(userenums.ErrInternal, "操作失败，请稍后重试")
}

// formValue 取表单字段并去空白（访客页面的表单字段全部是文本）。
func formValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// formBool 取复选框（HTML 表单未勾选时不提交该字段）。
func formBool(c *gin.Context, key string) bool {
	v := strings.ToLower(strings.TrimSpace(c.PostForm(key)))
	switch v {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// formInt 取整数字段，解析失败按默认值处理。
//
// 静默兜底在别处是坏味道，在这里是刻意的：这些字段（性别 / 每页条数）由页面上的
// 固定选项产生，能收到非法值只可能是手工构造的请求。为它返回一句「参数不合法」
// 不如直接用默认值 —— 真正的取值合法性由 service 兜底（每页条数越界会被拒）。
func formInt(c *gin.Context, key string, def int) int {
	v := strings.TrimSpace(c.PostForm(key))
	if v == "" {
		return def
	}
	n := 0
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return def
		}
		n = n*10 + int(v[i]-'0')
		if n > 1<<30 {
			return def
		}
	}
	return n
}

// clientIP 取访客真实 IP。
//
// 用 c.ClientIP()（gin 依据 TrustedProxies 决定是否采信 X-Forwarded-For）：
// release 模式下 TrustedProxies 为 nil，即不信任任何转发头 —— 这时 c.ClientIP()
// 返回的是直连地址。直接读 XFF 会让任何人都能伪造注册来源 IP。
func clientIP(c *gin.Context) string {
	return strings.TrimSpace(c.ClientIP())
}

// locale 取界面语言（访客页面目前只有 zh-CN，保留参数位是为了邮件模板的语言选择）。
func locale(c *gin.Context) string {
	if v := strings.TrimSpace(c.Query("lang")); v != "" {
		return v
	}
	return "zh-CN"
}

// isNotFound 判断是否为「没找到」类业务错误（用于决定 404 还是 400）。
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == userenums.ErrSessionNotFound || msg == userenums.ErrUserNotFound
}

var _ = errors.New

const (
	// ctxUserSession context 中存放当前访客会话的键。
	ctxUserSession = "user_auth_session"
)

// touchThrottleInterval 活跃时间写库的最小间隔。
//
// 不节流的话每个请求都会产生一条 UPDATE：一个页面带十几个资源请求就是十几次写。
// 活跃时间的精度要求本来就低（界面上显示「最近活跃」），60 秒完全够。
const touchThrottleInterval = 60 * time.Second

var (
	touchMu sync.Mutex
	// key 是会话令牌哈希（= 设备列表里的 id）：一台设备一次登录一个键，
	// 与原本按台账行 id 节流等价，而会话不再落库。
	touchSeen = map[string]time.Time{}
)

// shouldTouch 判断这台设备是否到了该更新活跃时间的时候。
func shouldTouch(sessionHash string) bool {
	if strings.TrimSpace(sessionHash) == "" {
		return false
	}
	now := time.Now()
	touchMu.Lock()
	defer touchMu.Unlock()
	if last, ok := touchSeen[sessionHash]; ok && now.Sub(last) < touchThrottleInterval {
		return false
	}
	touchSeen[sessionHash] = now
	// 顺手清理过期条目：这张表按「活跃会话行」增长，
	// 不清理的话长时间运行会攒下大量再也不会出现的会话哈希。
	if len(touchSeen) > 4096 {
		for id, t := range touchSeen {
			if now.Sub(t) > 10*touchThrottleInterval {
				delete(touchSeen, id)
			}
		}
	}
	return true
}

// attachUserSession 尽力解析访客会话并挂到 context，**不阻断请求**。
//
// 未登录不是错误：注册页、登录页本身就要在未登录时可访问。
// 需要登录的入口再叠 requireUser。
func attachUserSession(svc *userservice.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := readUserToken(c)
		if token == "" {
			c.Next()
			return
		}
		sess, _ := svc.ResolveSession(c.Request.Context(), token)
		if sess != nil {
			c.Set(ctxUserSession, sess)
			// 令牌也要挂到 context：登出与「退出其它设备」都要用它，
			// 而从 cookie 再读一次会让「cookie 已被清掉」这类边界出现分歧。
			c.Set(userSessionTokenKey, token)
			if shouldTouch(crypto.Sha256(token)) {
				svc.TouchSession(c.Request.Context(), sess)
			}
		}
		c.Next()
	}
}

// requireUser 要求已登录：页面请求 302 到登录页（带 next 回跳），其余返回 401。
//
// next 必须是本站路径（见 safeNext）：不加限制的话
// `/user/login?next=https://evil.example` 看起来就是本站的登录链接，
// 登录成功后把用户送去别处 —— 典型的开放重定向。
func requireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentSession(c) != nil {
			c.Next()
			return
		}
		if wantsJSON(c) {
			// JSON 响应统一走 pkg/response（模块规范：不自己拼 {code,message} 结构）。
			response.ErrorWithMessage(c, http.StatusUnauthorized, userenums.ErrNotLoggedIn)
			c.Abort()
			return
		}
		next := c.Request.URL.RequestURI()
		c.Redirect(http.StatusFound, "/user/login?next="+url.QueryEscape(next))
		c.Abort()
	}
}

// wantsJSON 判断调用方期望 JSON 而不是页面（HTMX / fetch 请求）。
func wantsJSON(c *gin.Context) bool {
	if c.GetHeader("HX-Request") != "" {
		return false // HTMX 要的是 HTML 片段
	}
	accept := c.GetHeader("Accept")
	return accept != "" && !strings.Contains(accept, "text/html")
}

// currentSession 取当前请求的访客会话；未登录返回 nil。
func currentSession(c *gin.Context) *userservice.UserAuthSession {
	v, ok := c.Get(ctxUserSession)
	if !ok {
		return nil
	}
	s, ok := v.(*userservice.UserAuthSession)
	if !ok {
		return nil
	}
	return s
}

// currentToken 取当前请求携带的会话令牌。
func currentToken(c *gin.Context) string {
	if v, ok := c.Get(userSessionTokenKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return readUserToken(c)
}

// safeNext 校验回跳地址：只允许本站绝对路径。
//
// 允许的形态：以单个 "/" 开头且不是 "//"（"//evil.com" 在浏览器里是协议相对 URL，
// 会跳到外站）。其余一律回落到 fallback。
func safeNext(next, fallback string) string {
	next = strings.TrimSpace(next)
	if next == "" || next[0] != '/' || (len(next) > 1 && next[1] == '/') {
		return fallback
	}
	// 反斜杠：历史浏览器把 `/\evil.com` 也当协议相对 URL 处理，这里一并挡掉，
	// 代价是拒绝一个几乎不存在的合法路径。
	if strings.Contains(next, "\\") {
		return fallback
	}
	return next
}

const (
	// userSessionCookieName 访客会话 cookie 名。
	//
	// **必须与后台的 gowp_session 不同**：同一个 cookie 只能存一份会话，
	// 共用会让「后台开着 + 前台登录一次」直接把管理员顶出后台。
	userSessionCookieName = "gowp_user_session"

	// userSessionTokenKey cookie 会话里存令牌的键。
	userSessionTokenKey = "user_token"
	// userCSRFKey CSRF token 在访客会话里的键。
	userCSRFKey = "csrf_token"

	// 有效期与 Redis 会话、user_sessions 台账口径一致（见 service.SessionTTLFor）。
	userSessionMaxAge         = 24 * 60 * 60
	userSessionRememberMaxAge = 7 * 24 * 60 * 60
)

var (
	userStoreMu     sync.RWMutex
	userCookieStore sessions.Store
)

// setupUserCookieStore 建访客的 cookie store（由 SetupUserRoutes 在装配期调用）。
//
// 用 pkg/auth 的 NamedCookieStore 而不是自己 new 一个 store：密钥与后台同源
// （同一个 auth.session_secret），差异只在 cookie 名。各建一份密钥会让轮换密钥时漏掉一个。
func setupUserCookieStore() error {
	store, err := auth.NamedCookieStore(userSessionCookieName)
	if err != nil {
		return err
	}
	userStoreMu.Lock()
	userCookieStore = store
	userStoreMu.Unlock()
	return nil
}

// userSession 取当前请求的访客 cookie 会话（gorilla 按请求缓存，同一请求多次调用返回同一对象）。
func userSession(c *gin.Context) (*gsessions.Session, error) {
	userStoreMu.RLock()
	store := userCookieStore
	userStoreMu.RUnlock()
	if store == nil {
		return nil, errors.New("访客会话存储未初始化")
	}
	return store.Get(c.Request, userSessionCookieName)
}

// readUserToken 读当前请求携带的会话令牌；没有则返回空串。
//
// 任何异常（存储未初始化、cookie 解码失败、类型不符）都按「没有令牌」处理 ——
// 调用方的语义是「这个请求有没有登录」，不是「为什么没有」。
func readUserToken(c *gin.Context) string {
	sess, err := userSession(c)
	if err != nil {
		return ""
	}
	raw, ok := sess.Values[userSessionTokenKey].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

// writeUserToken 写入会话令牌，rememberMe 决定 cookie 有效期（24h / 7d）。
func writeUserToken(c *gin.Context, token string, rememberMe bool) error {
	sess, err := userSession(c)
	if err != nil {
		return err
	}
	sess.Values[userSessionTokenKey] = token
	if rememberMe {
		sess.Options.MaxAge = userSessionRememberMaxAge
	} else {
		sess.Options.MaxAge = userSessionMaxAge
	}
	return sess.Save(c.Request, c.Writer)
}

// VisitorIdentityMiddleware 尽力解析访客会话，把 userID 挂到 gin context（**不阻断**）。
//
// 这是给访问面片段端点用的最小版本，与 attachUserSession 有三处刻意的差别：
//
//	· 只挂 user id，不挂会话对象 —— 片段层不需要昵称头像，也不需要撤销设备的行 id；
//	· **不做活跃时间续期**：片段请求量远大于页面请求，每个都 UPDATE 一次 user_sessions，
//	  是拿数据库写放大去换一个没人看的时间戳；
//	· 未登录不是错误 —— 访客未登录照样要看购物车计数与商品可用量，
//	  「必须登录」由具体片段能力自己声明（AuthVisitor）。
func VisitorIdentityMiddleware(svc usercontract.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// CSRF token 先于身份：片段渲染出的表单需要一个能通过校验的 token，
		// 而**未登录访客也要能提交登录 / 注册表单** —— 这条不能挂在「已登录」之后。
		// EnsureCSRFTokenWith 存在即复用，没有才生成并写会话（Set-Cookie 在片段端点上有效）。
		if csrf, err := builtin.EnsureCSRFTokenWith(c, userCSRFStore{}); err == nil && csrf != "" {
			c.Set(usercontract.VisitorCSRFContextKey, csrf)
		}
		if svc == nil {
			c.Next()
			return
		}
		token := readUserToken(c)
		if token == "" {
			c.Next()
			return
		}
		if id, ok := svc.ResolveVisitorID(c.Request.Context(), token); ok && id != 0 {
			c.Set(usercontract.VisitorContextKey, id)
		}
		// 令牌本身也挂上去：片段层要用它判定登录设备列表里「哪一台是当前设备」
		// （台账存的是令牌的 sha256，比对需要原值）。挂了不等于能用 —— 消费方
		// 只有 VisitorAccountPort.SessionsOf 一处，且不得把它渲染进输出。
		c.Set(usercontract.VisitorTokenContextKey, token)
		c.Next()
	}
}

// clearUserSession 清空访客 cookie 会话（登出：MaxAge=-1 让浏览器删掉它）。
func clearUserSession(c *gin.Context) error {
	sess, err := userSession(c)
	if err != nil {
		// 存储未初始化时无从清理，返回 nil（登出必须幂等）。
		return nil
	}
	sess.Values = make(map[interface{}]interface{})
	sess.Options.MaxAge = -1
	sess.Options.Path = "/"
	return sess.Save(c.Request, c.Writer)
}

// userCSRFStore 访客侧的 CSRF token 存取（实现在访客会话里，与后台互不干扰）。
//
// 必须独立：后台的 CSRF token 存在 gowp_session 中，访客写同一个键会把管理员的
// token 顶掉，于是后台下一个表单提交直接 403 —— 一个「登录一次前台就写不了后台」的怪现象。
type userCSRFStore struct{}

// Get 读取访客会话里的 CSRF token。
func (userCSRFStore) Get(c *gin.Context) string {
	sess, err := userSession(c)
	if err != nil {
		return ""
	}
	raw, ok := sess.Values[userCSRFKey].(string)
	if !ok {
		return ""
	}
	return raw
}

// Save 写入访客会话的 CSRF token。
func (userCSRFStore) Save(c *gin.Context, token string) error {
	sess, err := userSession(c)
	if err != nil {
		return err
	}
	sess.Values[userCSRFKey] = token
	return sess.Save(c.Request, c.Writer)
}

// customerMembershipView 客户详情页上「会员等级」那一块的渲染数据。
//
// 零值即「什么都没有」：没有等级、没有权益、没有降级说明 —— 页面据此渲染空块而不是报错。
type customerMembershipView struct {
	// HasMembership 解析出了会员身份（含默认等级兜底）。
	HasMembership bool
	// TierName 生效的等级名。
	TierName string
	// IsDefaultTier 本次是默认等级兜底（这个客户还没有归属行）。
	//
	// 与「他是这个等级的会员」必须能分辨：运营看这一块是要判断「该不该给他手工指定等级」。
	IsDefaultTier bool
	// FreeShipping 该等级免运费。
	FreeShipping bool
	// DiscountPercent 折扣扣减百分比（0 = 无折扣权益）。
	DiscountPercent int64
	// Notice 降级说明（非空时模板只显示它，不显示等级）。三种形态：
	// 端口未接入 / 没选工程 / 解析失败。
	Notice string
}

// SetMembershipDisplay 注入会员展示所需的两个端口（装配期调用）。
//
// 两个一起注入而不是两个 setter：它们服务的是同一块展示，只接一半的中间态
// （读得到等级、错误却直出原文）没有任何部署理由，而分开注入一定会有人只接一半。
func (h *customerPageHandle) SetMembershipDisplay(reader membershipcontract.Reader, texter membershipcontract.FacingTexter) {
	if h == nil {
		return
	}
	h.membership = reader
	h.membershipFacing = texter
}

// membershipView 取该客户在某工程下的会员展示数据。
//
// 客户 id 为 0 或工程为空即返回**带说明的空视图**：详情页是在某个工程上算的
// （消费额与等级都按工程算），没有工程就没有可展示的等级 —— 这是正常状态，
// 不是错误（客户管理页在没有站点工程时也长这样）。
func (h *customerPageHandle) membershipView(ctx context.Context, c *gin.Context, projectID string, userID uint64) customerMembershipView {
	tr := shell.TranslateFor(c)
	if h.membership == nil {
		// 装配缺失：说清是「会员模块没接」，而不是让运营以为这个客户没有等级。
		return customerMembershipView{Notice: tr(customerMembershipUnavailableLabel.key, customerMembershipUnavailableLabel.fallback)}
	}
	if userID == 0 || projectID == "" {
		return customerMembershipView{Notice: tr(customerMembershipNoProjectLabel.key, customerMembershipNoProjectLabel.fallback)}
	}
	member, err := h.membership.Resolve(ctx, &membershipdto.ResolveReq{ProjectID: projectID, UserID: userID})
	if err != nil || member == nil {
		// 原文只进日志（经 FacingTexter 归口）：客户页拿不到会员模块的 enums 白名单，
		// 直出 err.Error() 会把 PostgreSQL 原文漏到页面上。
		return customerMembershipView{Notice: h.membershipFacingText(c, err)}
	}
	return customerMembershipView{
		HasMembership:   true,
		TierName:        member.TierName,
		IsDefaultTier:   member.IsDefaultTier,
		FreeShipping:    member.FreeShipping,
		DiscountPercent: member.DiscountPercent,
	}
}

// membershipFacingText 会员模块错误的展示文案（拿不到出口时用本地兜底）。
func (h *customerPageHandle) membershipFacingText(c *gin.Context, err error) string {
	tr := shell.TranslateFor(c)
	fallback := tr(customerMembershipFailedLabel.key, customerMembershipFailedLabel.fallback)
	if h.membershipFacing == nil || err == nil {
		return fallback
	}
	if text := h.membershipFacing.FacingText(response.RequestLanguage(c), err); text != "" {
		return text
	}
	return fallback
}

// applyCustomerMembership 把会员视图写进详情页渲染数据。
//
// 为什么是「往已有的 gin.H 里写键」而不是给 customerDetailPageData 加参数：
// 那个组装函数有 6 处调用点，其中 4 处在渲染测试里 —— 为一块展示改签名会把测试
// 一起卷进来（而它本身没有变化），换来的只是少写一行传参。
//
// **渲染键恒设**（包括空值）是硬要求：Jet 的 `{{if .X}}` 遇到缺失的键会中断整页渲染
// → 500 → htmx 不 swap（用户在浏览器里看不到任何反应）。
func applyCustomerMembership(data gin.H, view customerMembershipView) {
	data["MembershipNotice"] = view.Notice
	data["HasMembership"] = view.HasMembership
	data["MembershipTierName"] = view.TierName
	data["MembershipIsDefault"] = view.IsDefaultTier
	data["MembershipFreeShipping"] = view.FreeShipping
	data["MembershipDiscountPercent"] = view.DiscountPercent
}

// 三个降级说明（i18n key + 中文兜底，与页面其余展示标签同形态）。
var (
	customerMembershipUnavailableLabel = userLabel{"admin.customer_detail.membership.unavailable", "会员模块尚未接入，这里看不到等级。"}
	customerMembershipNoProjectLabel   = userLabel{"admin.customer_detail.membership.no_project", "还没有站点工程：会员等级按工程计算，选定工程后才能看到。"}
	customerMembershipFailedLabel      = userLabel{"admin.customer_detail.membership.failed", "会员等级暂时读不出来 —— 客户资料本身不受影响。"}
)

// customer_query.go - 客户管理页的查询参数解析、URL 派生与对外文案出口。
//
// 本文件里的解析函数与 user_customer_admin_handle.go（/api/customer/*）里同名的一组
// 刻意各自独立：接口面与页面面的取值口径不同（页面用 -1 表示「全部」，接口用
// userdto.CustomerStatusAll），合并会逼着两边共用一份「谁改都得动」的实现。
// 名字因此带上 Page 前缀以区别于接口面那一组。

// customerDetailBackURL 「返回列表」链接：只保留筛选条件（把详情专属参数留在详情页）。
// c 为 nil 时退化成纯列表路径：渲染数据组装是纯函数，理应在没有请求上下文时也能跑
// （渲染测试就是直接喂数据走这条路径的），不该因为少一个 context 就 panic。
func customerDetailBackURL(c *gin.Context) string {
	q := url.Values{}
	if c != nil {
		for _, key := range []string{"keyword", "status", "emailVerified", "locked", "registeredFrom", "registeredTo", "page", "limit"} {
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

// customerPageFacingText 页面路径的提示取词出口（白名单判定 + **取当前语言的译文**）。
//
// 与订单页同因（见 internal/module/order/inbound/http/order_page_query.go 的
// orderPageFacingText）：userenums.UserFacingMessages 里存的是 item_key
// （user.msg.customerDisabled / user.err.userNotFound …），而页面模板 customers.html /
// customer_detail.html 里的 {{.Err}} 是**直接渲染**的文本、不经过 pkg/response 的
// translate —— 只放行 key 的话，运营看到的就是「user.err.userNotFound」。
//
// 白名单判定仍只有一份（customerFacingText），本函数只把命中的值按当前语言取词。
// 写动作的成功 / 失败文案现在由 shell.RenderJump 渲染（见 customerJumpDone / customerJumpFail），
// 本函数只剩页面取数失败（customerFacingError）这一条消费路径。
func customerPageFacingText(c *gin.Context) func(string) string {
	return func(raw string) string {
		hit := customerFacingText(raw)
		if hit == "" {
			return ""
		}
		return shell.TranslateFor(c)(hit, hit)
	}
}

// customerFacingError 把 user 模块的错误转成可展示文案（后台客户页的错误文案归口出口）。
//
// 三件套（对齐 AGENTS.md「响应与错误处理」，样板见 admin_err.go / navigation_err.go）：
//
//	· 白名单 —— customerFacingText（userenums.UserFacingMessages）；
//	· 归口文案 —— shell.PageInternalText(c)（MsgInternalError 的当前语言译文）；
//	· 结构化日志 —— 未命中时记一条带场景 / user_id / 路径的日志，原文只进日志。
//
// 未命中的通常是数据库 / Redis 错误的 Error()，带表名甚至 SQL 片段，那是给运维看的；
// 页面上给一句通用提示，日志里留全文 —— 否则「页面上什么都没说」会变成最难查的一类问题
// （这一条此前缺失：函数直接返回了归口文案而没有记日志）。
//
// 命中那一支经 customerPageFacingText 取译文（白名单里是 item_key）。
func customerFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := customerPageFacingText(c)(err.Error()); msg != "" {
		return msg
	}
	logger.Scene(userErrScene).
		With("user_id", shell.CurrentUserID(c)).
		With("path", c.Request.URL.Path).
		Error(err, "后台客户页处理失败")
	return shell.PageInternalText(c)
}

// userErrScene 后台客户页的日志场景名（与 user 模块其它 logger.Scene("user") 调用点一致）。
const userErrScene = "user"

// customerBulkIDsText shell.BulkIDs 的失败文案（单次提交的 id 超过上限）。
//
// 只是转调 shell 的受控出口：超限错误是 shell 的类型（shell.BulkIDsError），
// 「一次最多操作 N 项，当前 M 项，请分批进行」按当前语言生成，其中**当前 M 项**
// （去重后的条数）只有 shell 知道 —— 本模块不再用 shell.MaxBulkIDs 重算一遍：
// 那是第二份真相，而且必然丢掉 Count（旧的实现正是如此）。
// 判据也不再是「文案来自哪里」而是类型：出口只认 sentinel，认不出就回落归口文案。
// 留痕（哪个操作人、哪个页面触发）由 shell 的出口统一记日志。
func customerBulkIDsText(c *gin.Context, err error) string {
	return shell.BulkIDsFacingText(c, err)
}

// customerFacingText 白名单校验：命中返回原文，未命中返回空串。
//
// 只做判定、不取词：白名单里存的是 item_key，页面出口用 customerPageFacingText 取译文。
// 判定只有这一份（只认 userenums.UserFacingMessages —— 本页自造的文案现在直接经
// userLabelOf 渲染，不再经 ?err= 回显，所以不需要第二部分受控集合）。
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
	return ""
}

// customerFirstNonEmpty 取第一个非空文案（多处「提示只留第一条」的收口）。
func customerFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// customerQueryID 解析 id / customerId（非法即 0）。
func customerQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// customerPageStatus 解析状态（空串或非法一律「全部」）。
//
// 空串**不能**落成 0：0 是「已停用」，那会让不带参数的请求只看到停用账号。
func customerPageStatus(raw string) int {
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

// customerPageLocked 解析「只看锁定」筛选（只有 ?locked=1 为真）。
//
// 与状态筛选是两条轴：被锁定的账号 status 仍是「正常」（锁定只写 locked_until_time），
// 所以它必须是一个独立的查询参数，不能拿状态下拉去表达。
func customerPageLocked(raw string) bool {
	return strings.TrimSpace(raw) == "1"
}

// customerPageEmailVerified 解析邮箱验证筛选（空串 → 全部）。
func customerPageEmailVerified(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1":
		return userdto.EmailVerifiedYes
	case "2":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

// customerPageDayStart / customerPageDayEnd 日期字符串 → 当天的起止时刻（解析不了返回 nil = 该端不限）。
//
// 结束日期必须扩到当天最后一刻：把 2026-09-30 当成 00:00:00，那一天注册的客户
// 一个都筛不出来，而运营以为自己筛的是「到 9 月 30 日为止」—— 少一天看起来完全正常。
//
// 解析失败按「不限」处理而不是报错：这是筛选条件，拼错了退化成不筛，
// 比让整页变成错误页更接近运营的预期（他至少还看得到列表）。
func customerPageDayStart(raw string) *time.Time {
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

func customerPageDayEnd(raw string) *time.Time {
	day := customerPageDayStart(raw)
	if day == nil {
		return nil
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end
}

// accountNextTarget 取「保存类操作完成后的站内回跳目标」；没有或不可信则返回空串。
//
// 为什么需要它：账号表单现在是**片段**，可以出现在作者自己排的账号页上。
// 提交后停在内置账号页，会让作者页面上的账号中心半途跳到另一个页面 ——
// 那正好抵消了「把账号功能放到自己页面上」的意义。
//
// 为什么必须校验而不是照抄表单值：直接把 next 塞进 Location 就是**开放重定向**，
// 站点会变成任意目的地的跳板，而页面看起来一切正常，没人会去查。
// 规则取最保守的一档（站内相对路径），够用且不留解释空间：
//   - 必须以单个 / 开头（//evil.com 是协议相对 URL，会被浏览器当外部站点）；
//   - 不得含反斜杠（多数浏览器把它归一成正斜杠，于是「斜杠 + 反斜杠 + 主机名」会被解析成站外地址）；
//   - 不得含控制字符（换行会污染响应头）；
//   - 长度封顶，避免把超长串带进 Location。
func accountNextTarget(c *gin.Context) string {
	next := strings.TrimSpace(c.PostForm("next"))
	if next == "" || len(next) > 512 {
		return ""
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	if strings.ContainsAny(next, "\\\r\n") {
		return ""
	}
	return next
}

// redirectAfterSave 保存成功后的落点：带了合法的站内 next 就回跳，否则留在内置账号页。
//
// 只在**成功**路径上回跳：失败必须把错误显示出来，而错误现在只有内置账号页会渲染。
// 把失败也送走，用户会得到一个「点了保存、页面刷新了、什么都没变」的界面。
func redirectAfterSave(c *gin.Context) bool {
	target := accountNextTarget(c)
	if target == "" {
		return false
	}
	c.Redirect(http.StatusFound, target)
	return true
}

// ShowAccount 账号中心。
func (h *Handle) ShowAccount(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		// 理论上 requireUser 已经挡掉，这里是纵深防御：
		// 少一次判空就少一处「改装配顺序时静默变成 500」的地方。
		c.Redirect(http.StatusFound, "/user/login?next=%2Fuser%2Faccount")
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// renderAccount 渲染账号中心（actionError 非空时在页面上显示一条错误）。
func (h *Handle) renderAccount(c *gin.Context, status int, actionError string) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login?next=%2Fuser%2Faccount")
		return
	}
	account, err := h.svc.GetAccount(c.Request.Context(), sess.UserID)
	if err != nil {
		h.renderMessage(c, http.StatusInternalServerError, false, userTextOf(c, userMsgAccountOpenFailedTitle), userPageMessage(c, err))
		return
	}
	sessions, serr := h.svc.ListSessions(c.Request.Context(), sess.UserID, currentToken(c))
	if serr != nil {
		// 设备列表拉不到不该让整页打不开：资料与偏好仍然可用。
		sessions = nil
	}
	h.render(c, status, "user/account", gin.H{
		"account":     account,
		"sessions":    sessions,
		"actionError": actionError,
	})
}

// DoUpdateProfile 保存资料。
func (h *Handle) DoUpdateProfile(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	err := h.svc.UpdateProfile(c.Request.Context(), &userdto.UpdateProfileReq{
		UserID:    sess.UserID,
		Nickname:  formValue(c, "nickname"),
		FirstName: formValue(c, "firstName"),
		LastName:  formValue(c, "lastName"),
		Gender:    formInt(c, "gender", 0),
		Birthday:  formValue(c, "birthday"),
		Bio:       formValue(c, "bio"),
		Website:   formValue(c, "website"),
		Locale:    formValue(c, "locale"),
		Timezone:  formValue(c, "timezone"),
		Country:   formValue(c, "country"),
		Province:  formValue(c, "province"),
		City:      formValue(c, "city"),
		Address:   formValue(c, "address"),
		Postcode:  formValue(c, "postcode"),
		Phone:     formValue(c, "phone"),
		Company:   formValue(c, "company"),
	})
	if err != nil {
		h.renderAccount(c, http.StatusBadRequest, userPageMessage(c, err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// DoUpdatePreference 保存偏好。
func (h *Handle) DoUpdatePreference(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	err := h.svc.UpdatePreference(c.Request.Context(), &userdto.UpdatePreferenceReq{
		UserID:            sess.UserID,
		Theme:             formValue(c, "theme"),
		Locale:            formValue(c, "locale"),
		Timezone:          formValue(c, "timezone"),
		PageSize:          formInt(c, "pageSize", 20),
		EmailNotify:       formBool(c, "emailNotify"),
		SmsNotify:         formBool(c, "smsNotify"),
		ProfileVisibility: formValue(c, "profileVisibility"),
		ShowOnline:        formBool(c, "showOnline"),
	})
	if err != nil {
		h.renderAccount(c, http.StatusBadRequest, userPageMessage(c, err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// DoChangePassword 修改密码。
//
// 改密成功后 service 会撤销该用户的**全部**会话（含当前这一个），
// 所以这里必须清 cookie 并把人送回登录页 —— 留着 cookie 会让人看到
// 「还在账号中心、但每个操作都跳登录页」的诡异状态。
func (h *Handle) DoChangePassword(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	err := h.svc.ChangePassword(c.Request.Context(), &userdto.ChangePasswordReq{
		UserID:      sess.UserID,
		OldPassword: c.PostForm("oldPassword"),
		NewPassword: c.PostForm("newPassword"),
	})
	if err != nil {
		h.renderAccount(c, http.StatusBadRequest, userPageMessage(c, err))
		return
	}
	_ = clearUserSession(c)
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgPasswordChangedTitle),
		userTextOf(c, userMsgPasswordChangedBody))
}

// DoRevokeSession 踢掉某台设备。
func (h *Handle) DoRevokeSession(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	// 设备标识是会话令牌的 sha256（Redis 索引成员），不是数据库行 id —— 会话不落库。
	sessionHash := strings.TrimSpace(formValue(c, "id"))
	if sessionHash == "" {
		h.renderAccount(c, http.StatusBadRequest, userKeyText(c, userenums.ErrSessionNotFound))
		return
	}
	if err := h.svc.RevokeSession(c.Request.Context(), sess.UserID, sessionHash); err != nil {
		h.renderAccount(c, http.StatusBadRequest, userPageMessage(c, err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// DoRevokeOtherSessions 退出其它所有设备。
func (h *Handle) DoRevokeOtherSessions(c *gin.Context) {
	sess := currentSession(c)
	if sess == nil {
		c.Redirect(http.StatusFound, "/user/login")
		return
	}
	if _, err := h.svc.RevokeOtherSessions(c.Request.Context(), sess.UserID, currentToken(c)); err != nil {
		h.renderAccount(c, http.StatusBadRequest, userPageMessage(c, err))
		return
	}
	if redirectAfterSave(c) {
		return
	}
	h.renderAccount(c, http.StatusOK, "")
}

// 访客结果页（user/message）的标题与正文（key + 中文兜底）。
//
// 这些文本是**直接渲染**进页面的（renderMessage 把成品文案塞进 gin.H 的 title/message），
// 硬写中文等于英文界面恒中文。带变量的正文用 %s 占位 —— 词条协议只允许字符串占位符，
// 因此变量先经 fmt.Sprintf 字符串化（用户名 / 邮箱本来就是字符串，无需转换）。
var (
	userMsgActivationInvalidTitle = userLabel{"user.message.activation_invalid.title", "验证链接无效"}
	userMsgActivationFailedTitle  = userLabel{"user.message.activation_failed.title", "验证未通过"}
	// userMsgActivatedTitle 复用访客消息词条 user.msg.activateSuccess（值同为「邮箱验证成功」）：
	// 同一句话在消息通道与结果页各留一条词条，翻译改动时必然分叉。
	userMsgActivatedTitle = userLabel{userenums.MsgActivateSuccess, "邮箱验证成功"}
	userMsgActivatedBody  = userLabel{"user.message.activated.body", "账号 {username} 已激活，现在可以登录了。"}

	userMsgResendFailedTitle = userLabel{"user.message.resend_failed.title", "重发失败"}
	userMsgResendDoneTitle   = userLabel{"user.message.resend_done.title", "验证邮件已重发"}
	userMsgResendDoneBody    = userLabel{"user.message.resend_done.body",
		"如果 {email} 是一个待验证的账号，新的验证邮件已经发出，请查收。"}

	userMsgLogoutFailedTitle = userLabel{"user.message.logout_failed.title", "登出失败"}
	userMsgResetMailTitle    = userLabel{"user.message.reset_mail.title", "重置邮件已提交"}
	userMsgLinkInvalidTitle  = userLabel{"user.message.link_invalid.title", "链接无效"}

	userMsgPasswordResetTitle = userLabel{"user.message.password_reset.title", "密码已重置"}
	userMsgPasswordResetBody  = userLabel{"user.message.password_reset.body", "请使用新密码登录。"}

	userMsgAccountOpenFailedTitle = userLabel{"user.message.account_open_failed.title", "打不开账号中心"}
	userMsgPasswordChangedTitle   = userLabel{"user.message.password_changed.title", "密码已修改"}
	userMsgPasswordChangedBody    = userLabel{"user.message.password_changed.body",
		"为安全起见，所有设备（包括当前这台）都已退出登录，请用新密码重新登录。"}
)

// ShowRegister 注册页。
func (h *Handle) ShowRegister(c *gin.Context) {
	if currentSession(c) != nil {
		// 已登录还去注册页，说明多半是点了旧书签：直接送回账号中心，
		// 不显示一个「注册新账号」的表单让人以为自己没登录。
		c.Redirect(http.StatusFound, "/user/account")
		return
	}
	h.render(c, http.StatusOK, "user/register", gin.H{
		"form": emptyRegisterForm(),
	})
}

// DoRegister 提交注册。
func (h *Handle) DoRegister(c *gin.Context) {
	form := gin.H{
		"username": formValue(c, "username"),
		"email":    formValue(c, "email"),
		"nickname": formValue(c, "nickname"),
	}
	req := &userdto.RegisterReq{
		Username:   form["username"].(string),
		Email:      form["email"].(string),
		Password:   c.PostForm("password"),
		Nickname:   form["nickname"].(string),
		RegisterIP: clientIP(c),
		Locale:     locale(c),
	}
	res, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		// 失败时**回填已填内容**（不含密码）：让人重新把所有字段打一遍是最容易劝退的一步。
		h.render(c, http.StatusBadRequest, "user/register", gin.H{
			"error": userPageMessage(c, err),
			"form":  form,
		})
		return
	}
	h.render(c, http.StatusOK, "user/register_done", gin.H{
		"username": res.Username,
		"email":    res.Email,
		// mailQueued=false 时页面要提示「可以点重发」——邮件没发出去时
		// 只显示「验证邮件已发送」会让用户一直等一封不会到的信。
		"mailQueued": res.MailQueued,
	})
}

// Activate 邮箱验证（GET，链接来自邮件）。
func (h *Handle) Activate(c *gin.Context) {
	key := formValue(c, "key")
	if key == "" {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgActivationInvalidTitle), userKeyText(c, userenums.ErrActivationInvalid))
		return
	}
	res, err := h.svc.ActivateEmail(c.Request.Context(), &userdto.ActivateEmailReq{
		Key:    key,
		Locale: locale(c),
	})
	if err != nil {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgActivationFailedTitle), userPageMessage(c, err))
		return
	}
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgActivatedTitle),
		userTextFilled(c, userMsgActivatedBody, map[string]string{"username": res.Username}))
}

// DoResendActivation 重发验证邮件。
func (h *Handle) DoResendActivation(c *gin.Context) {
	email := formValue(c, "email")
	if email == "" {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgResendFailedTitle), userKeyText(c, userenums.ErrEmailRequired))
		return
	}
	if err := h.svc.ResendActivation(c.Request.Context(), &userdto.ResendActivationReq{
		Email:  email,
		Locale: locale(c),
	}); err != nil {
		// 与密码重置不同，这里**如实报错**：重发接口要求填的邮箱本身就能通过注册接口
		// 探测出是否被占用，在这里沉默不会多保护任何信息，只会让「邮箱打错了」的人
		// 盯着「已发送」干等。
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgResendFailedTitle), userPageMessage(c, err))
		return
	}
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgResendDoneTitle),
		userTextFilled(c, userMsgResendDoneBody, map[string]string{"email": email}))
}

// ShowLogin 登录页。
func (h *Handle) ShowLogin(c *gin.Context) {
	if currentSession(c) != nil {
		c.Redirect(http.StatusFound, "/user/account")
		return
	}
	h.render(c, http.StatusOK, "user/login", gin.H{
		"next":    c.Query("next"),
		"account": "",
	})
}

// DoLogin 提交登录。
func (h *Handle) DoLogin(c *gin.Context) {
	account := formValue(c, "account")
	next := safeNext(c.PostForm("next"), "/user/account")

	res, err := h.svc.Login(c.Request.Context(), &userdto.LoginReq{
		Account:    account,
		Password:   c.PostForm("password"),
		RememberMe: formBool(c, "remember"),
	}, userservice.SessionMeta{
		IP:        clientIP(c),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.render(c, http.StatusUnauthorized, "user/login", gin.H{
			"error":   userPageMessage(c, err),
			"account": account,
			"next":    next,
		})
		return
	}

	if werr := writeUserToken(c, res.Token, formBool(c, "remember")); werr != nil {
		// cookie 写不进去 → 会话虽然是真建了的，但这个浏览器拿不到令牌。
		// 提示重新登录比「看着像登录成功、下一页又回到登录页」好排查。
		h.render(c, http.StatusInternalServerError, "user/login", gin.H{
			"error":   userenums.ErrInternal,
			"account": account,
			"next":    next,
		})
		return
	}

	// 登录成功必须**轮换 CSRF token**：匿名阶段会话里的 token 可能已被预置
	//（子域 Set-Cookie 注入），沿用旧值会让攻击者预知登录后的 token。
	if _, rerr := builtin.RotateCSRFTokenWith(c, userCSRFStore{}); rerr != nil {
		// 轮换失败不阻断登录：用户已经登录成功，此处只是 CSRF token 没换新。
		// 下一次渲染表单时 EnsureCSRFTokenWith 会补一个。
		_ = rerr
	}

	c.Redirect(http.StatusFound, next)
}

// Logout 登出（POST：GET 登出会被浏览器预取或图片标签意外触发）。
func (h *Handle) Logout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), currentToken(c)); err != nil {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgLogoutFailedTitle), userPageMessage(c, err))
		return
	}
	// 先撤销服务端会话再清 cookie：反过来的话，若撤销失败，用户看到的是
	// 「已经登出」（cookie 没了），但服务端会话仍然有效。
	if err := clearUserSession(c); err != nil {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgLogoutFailedTitle), userKeyText(c, userenums.ErrInternal))
		return
	}
	c.Redirect(http.StatusFound, "/user/login")
}

// ShowForgot 申请重置密码页。
func (h *Handle) ShowForgot(c *gin.Context) {
	h.render(c, http.StatusOK, "user/forgot", gin.H{
		"email": c.Query("email"),
	})
}

// DoForgot 提交重置申请。
//
// **无论邮箱是否存在都显示同一句成功文案**：这个接口是匿名的、只需一个字段，
// 如实报错就等于提供了一个「查这个邮箱注册过没有」的接口。
func (h *Handle) DoForgot(c *gin.Context) {
	email := formValue(c, "email")
	if email == "" {
		h.render(c, http.StatusBadRequest, "user/forgot", gin.H{
			"error": userenums.ErrEmailRequired,
			"email": email,
		})
		return
	}
	if err := h.svc.RequestPasswordReset(c.Request.Context(), &userdto.PasswordResetReqRequest{
		Email:  email,
		Locale: locale(c),
	}); err != nil {
		h.render(c, http.StatusBadRequest, "user/forgot", gin.H{
			"error": userPageMessage(c, err),
			"email": email,
		})
		return
	}
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgResetMailTitle), userKeyText(c, userenums.MsgResetMailSent))
}

// ShowReset 重置密码页（链接来自邮件，带 email + key）。
func (h *Handle) ShowReset(c *gin.Context) {
	email := formValue(c, "email")
	key := formValue(c, "key")
	if email == "" || key == "" {
		h.renderMessage(c, http.StatusBadRequest, false, userTextOf(c, userMsgLinkInvalidTitle), userKeyText(c, userenums.ErrActivationInvalid))
		return
	}
	h.render(c, http.StatusOK, "user/reset", gin.H{
		"email": email,
		"key":   key,
	})
}

// DoReset 提交新密码。
func (h *Handle) DoReset(c *gin.Context) {
	email := formValue(c, "email")
	key := formValue(c, "key")
	form := gin.H{"email": email, "key": key}

	err := h.svc.ResetPassword(c.Request.Context(), &userdto.ResetPasswordReq{
		Email:       email,
		Key:         key,
		NewPassword: c.PostForm("password"),
	})
	if err != nil {
		h.render(c, http.StatusBadRequest, "user/reset", gin.H{
			"error": userPageMessage(c, err),
			"email": email,
			"key":   key,
		})
		return
	}
	_ = form
	h.renderMessage(c, http.StatusOK, true, userTextOf(c, userMsgPasswordResetTitle), userTextOf(c, userMsgPasswordResetBody))
}

// renderMessage 渲染通用结果页（激活 / 重发 / 重置这类一次性结果）。
//
// 不为每种结果写一个模板：它们结构完全相同（一个标题 + 一段话 + 一个去处），
// 拆成五个模板只会让「改一次文案要改五个文件」。
func (h *Handle) renderMessage(c *gin.Context, status int, ok bool, title, message string) {
	h.render(c, status, "user/message", gin.H{
		"ok":      ok,
		"title":   title,
		"message": message,
	})
}

// emptyRegisterForm 注册表单的初始值（模板用 chain 索引取值，键必须齐全）。
func emptyRegisterForm() gin.H {
	return gin.H{"username": "", "email": "", "nickname": ""}
}
